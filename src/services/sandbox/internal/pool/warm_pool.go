package pool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/services/sandbox/internal/firecracker"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
)

// Config 持有 WarmPool 运行所需的全部参数。
type Config struct {
	// 各 tier 预热数量，0 表示不预热
	WarmSizes             map[string]int
	RefillIntervalSeconds int
	MaxRefillConcurrency  int

	// VM 创建依赖
	FirecrackerBin     string
	KernelImagePath    string
	RootfsPath         string
	SocketBaseDir      string
	BootTimeoutSeconds int
	GuestAgentPort     uint32
	SnapshotStore      storage.SnapshotStore
	Registry           *template.Registry
	Logger             *logging.JSONLogger
}

// Entry 是 WarmPool 内部管理的 VM 实例。
type entry struct {
	session *session.Session
	process *os.Process
}

// Stats 是 WarmPool 的运行时统计。
type Stats = session.PoolStats

// WarmPool 管理预热的 Firecracker microVM，按 tier 分组。
type WarmPool struct {
	cfg   Config
	ready map[string]chan *entry
	sem   chan struct{} // 全局创建并发控制
	stop  chan struct{}
	wg    sync.WaitGroup

	totalCreated   atomic.Int64
	totalDestroyed atomic.Int64
}

// New 创建 WarmPool 实例（不启动后台 goroutine）。
func New(cfg Config) *WarmPool {
	ready := make(map[string]chan *entry)
	for tier, size := range cfg.WarmSizes {
		if size > 0 {
			ready[tier] = make(chan *entry, size)
		}
	}
	return &WarmPool{
		cfg:   cfg,
		ready: ready,
		sem:   make(chan struct{}, cfg.MaxRefillConcurrency),
		stop:  make(chan struct{}),
	}
}

// Start 启动所有 tier 的后台 refiller goroutine。
func (p *WarmPool) Start() {
	for tier, target := range p.cfg.WarmSizes {
		if target <= 0 {
			continue
		}
		p.wg.Add(1)
		go p.refiller(tier, target)
	}
}

// Acquire 获取一个就绪的 VM。优先从 warm pool 取，pool 为空时按需创建。
func (p *WarmPool) Acquire(ctx context.Context, tier string) (*session.Session, *os.Process, error) {
	if ch, ok := p.ready[tier]; ok {
		select {
		case e := <-ch:
			return e.session, e.process, nil
		default:
		}
	}
	e, err := p.createVM(ctx, tier)
	if err != nil {
		return nil, nil, err
	}
	return e.session, e.process, nil
}

// Ready 返回所有启用了预热的 tier 是否已达到目标数量。
func (p *WarmPool) Ready() bool {
	for tier, target := range p.cfg.WarmSizes {
		if target <= 0 {
			continue
		}
		ch, ok := p.ready[tier]
		if !ok || len(ch) < target {
			return false
		}
	}
	return true
}

// Stats 返回当前运行时统计。
func (p *WarmPool) Stats() Stats {
	readyByTier := make(map[string]int)
	targetByTier := make(map[string]int)
	for tier, target := range p.cfg.WarmSizes {
		targetByTier[tier] = target
		if ch, ok := p.ready[tier]; ok {
			readyByTier[tier] = len(ch)
		}
	}
	return Stats{
		ReadyByTier:    readyByTier,
		TargetByTier:   targetByTier,
		TotalCreated:   p.totalCreated.Load(),
		TotalDestroyed: p.totalDestroyed.Load(),
	}
}

// Drain 停止所有 refiller 并销毁所有预热 VM。Graceful shutdown 时调用。
func (p *WarmPool) Drain(ctx context.Context) {
	close(p.stop)
	p.wg.Wait()
	for _, ch := range p.ready {
		p.drainChannel(ch)
	}
}

// DestroyVM 销毁一个 VM：kill 进程 + 清理 socket 目录。
func (p *WarmPool) DestroyVM(proc *os.Process, socketDir string) {
	if proc != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}
	if socketDir != "" {
		_ = os.RemoveAll(socketDir)
	}
	p.totalDestroyed.Add(1)
}

func (p *WarmPool) destroyEntry(e *entry) {
	if e == nil {
		return
	}
	p.DestroyVM(e.process, e.session.SocketDir)
}

func (p *WarmPool) drainChannel(ch chan *entry) {
	for {
		select {
		case e := <-ch:
			p.destroyEntry(e)
		default:
			return
		}
	}
}

func (p *WarmPool) refiller(tier string, target int) {
	defer p.wg.Done()

	p.fillTier(tier, target)

	ticker := time.NewTicker(time.Duration(p.cfg.RefillIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.fillTier(tier, target)
		}
	}
}

func (p *WarmPool) fillTier(tier string, target int) {
	ch := p.ready[tier]
	for len(ch) < target {
		select {
		case <-p.stop:
			return
		case p.sem <- struct{}{}:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		e, err := p.createVM(ctx, tier)
		cancel()
		<-p.sem

		if err != nil {
			p.cfg.Logger.Warn("warm pool refill failed", logging.LogFields{},
				map[string]any{"tier": tier, "error": err.Error()})
			return
		}

		select {
		case ch <- e:
		case <-p.stop:
			p.destroyEntry(e)
			return
		}
	}
}

// createVM 创建一个就绪的 VM 实例。优先走快照恢复，失败则冷启动。
func (p *WarmPool) createVM(ctx context.Context, tier string) (*entry, error) {
	var tmpl *template.Template
	if p.cfg.Registry != nil {
		if t, ok := p.cfg.Registry.ForTier(tier); ok {
			tmpl = &t
		}
	}

	if p.cfg.SnapshotStore != nil && tmpl != nil {
		if entry, err := p.createFromSnapshot(ctx, tier, tmpl); err == nil {
			return entry, nil
		}
	}
	return p.createCold(ctx, tier, tmpl)
}

func (p *WarmPool) createFromSnapshot(ctx context.Context, tier string, tmpl *template.Template) (*entry, error) {
	exists, err := p.cfg.SnapshotStore.Exists(ctx, tmpl.ID)
	if err != nil || !exists {
		return nil, fmt.Errorf("snapshot not available for %q", tmpl.ID)
	}

	id := generateID()
	socketDir := filepath.Join(p.cfg.SocketBaseDir, id)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	apiSocket := filepath.Join(socketDir, "api.sock")
	vsockPath := filepath.Join(socketDir, "vsock.sock")

	memPath, diskPath, err := p.cfg.SnapshotStore.Download(ctx, tmpl.ID)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("download snapshot: %w", err)
	}

	proc, err := p.startFirecracker(apiSocket)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	cleanup := func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
	}

	if err := firecracker.WaitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
		cleanup()
		return nil, fmt.Errorf("firecracker api socket: %w", err)
	}

	client := firecracker.NewClient(apiSocket)
	if err := client.LoadSnapshot(ctx, diskPath, memPath, true); err != nil {
		cleanup()
		return nil, fmt.Errorf("load snapshot: %w", err)
	}

	s := &session.Session{
		ID:        id,
		Tier:      tier,
		VsockPath: vsockPath,
		AgentPort: p.cfg.GuestAgentPort,
		CreatedAt: time.Now(),
		SocketDir: socketDir,
	}

	const snapshotAgentTimeout = 5 * time.Second
	if err := session.WaitForAgent(ctx, s, snapshotAgentTimeout); err != nil {
		cleanup()
		return nil, fmt.Errorf("guest agent not ready after snapshot restore: %w", err)
	}

	p.totalCreated.Add(1)
	return &entry{session: s, process: proc}, nil
}

func (p *WarmPool) createCold(ctx context.Context, tier string, tmpl *template.Template) (*entry, error) {
	kernelPath := p.cfg.KernelImagePath
	rootfsPath := p.cfg.RootfsPath
	if tmpl != nil {
		kernelPath = tmpl.KernelImagePath
		rootfsPath = tmpl.RootfsPath
	}

	id := generateID()
	socketDir := filepath.Join(p.cfg.SocketBaseDir, id)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	apiSocket := filepath.Join(socketDir, "api.sock")
	vsockPath := filepath.Join(socketDir, "vsock.sock")

	proc, err := p.startFirecracker(apiSocket)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	cleanup := func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
	}

	if err := firecracker.WaitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
		cleanup()
		return nil, fmt.Errorf("firecracker api socket: %w", err)
	}

	tierCfg := firecracker.TierFor(tier)
	client := firecracker.NewClient(apiSocket)
	if err := client.Configure(ctx,
		firecracker.MachineConfig{VcpuCount: tierCfg.VCPUCount, MemSizeMib: tierCfg.MemSizeMiB, Smt: false},
		firecracker.BootSource{KernelImagePath: kernelPath, BootArgs: firecracker.KernelArgs},
		firecracker.Drive{DriveID: "rootfs", PathOnHost: rootfsPath, IsRootDevice: true, IsReadOnly: true},
		firecracker.VsockDevice{GuestCID: 3, UDSPath: vsockPath},
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("configure microvm: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("start microvm: %w", err)
	}

	s := &session.Session{
		ID:        id,
		Tier:      tier,
		VsockPath: vsockPath,
		AgentPort: p.cfg.GuestAgentPort,
		CreatedAt: time.Now(),
		SocketDir: socketDir,
	}

	bootTimeout := time.Duration(p.cfg.BootTimeoutSeconds) * time.Second
	if err := session.WaitForAgent(ctx, s, bootTimeout); err != nil {
		cleanup()
		return nil, fmt.Errorf("guest agent not ready: %w", err)
	}

	p.totalCreated.Add(1)
	return &entry{session: s, process: proc}, nil
}

func (p *WarmPool) startFirecracker(apiSocket string) (*os.Process, error) {
	cmd := exec.Command(p.cfg.FirecrackerBin,
		"--api-sock", apiSocket,
		"--log-level", "Error",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "warm-" + hex.EncodeToString(b)
}
