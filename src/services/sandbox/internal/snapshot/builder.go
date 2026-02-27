package snapshot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"arkloop/services/sandbox/internal/firecracker"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
)

// Builder 封装冷启动 microVM → 打全量快照 → 上传 MinIO 的完整流程。
type Builder struct {
	firecrackerBin     string
	socketBaseDir      string
	bootTimeoutSeconds int
	guestAgentPort     uint32
	store              storage.SnapshotStore
	logger             *logging.JSONLogger
}

// NewBuilder 创建 Builder。所有参数均必须非零。
func NewBuilder(
	firecrackerBin, socketBaseDir string,
	bootTimeoutSeconds int,
	guestAgentPort uint32,
	store storage.SnapshotStore,
	logger *logging.JSONLogger,
) *Builder {
	return &Builder{
		firecrackerBin:     firecrackerBin,
		socketBaseDir:      socketBaseDir,
		bootTimeoutSeconds: bootTimeoutSeconds,
		guestAgentPort:     guestAgentPort,
		store:              store,
		logger:             logger,
	}
}

// Build 对单个 template 执行：冷启动 microVM → Guest Agent 就绪 → 打快照 → 上传 MinIO → 销毁 VM。
func (b *Builder) Build(ctx context.Context, tmpl template.Template) error {
	b.logger.Info("building snapshot", logging.LogFields{}, map[string]any{"template_id": tmpl.ID})

	// 在 socketBaseDir 下创建专用临时目录
	buildDir := filepath.Join(b.socketBaseDir, "_builder_"+tmpl.ID)
	if err := os.MkdirAll(buildDir, 0o700); err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}

	var proc *os.Process
	cleanup := func() {
		if proc != nil {
			_ = proc.Kill()
			_, _ = proc.Wait()
		}
		_ = os.RemoveAll(buildDir)
	}
	defer cleanup()

	apiSocket := filepath.Join(buildDir, "api.sock")
	vsockPath := filepath.Join(buildDir, "vsock.sock")

	// 启动 Firecracker 进程
	var err error
	proc, err = startProcess(b.firecrackerBin, apiSocket)
	if err != nil {
		return fmt.Errorf("start firecracker: %w", err)
	}

	// 等待 API socket 就绪
	if err := firecracker.WaitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
		return fmt.Errorf("api socket: %w", err)
	}

	// 配置并启动 microVM
	tierCfg := firecracker.TierFor(tmpl.Tier)
	client := firecracker.NewClient(apiSocket)
	if err := client.Configure(ctx,
		firecracker.MachineConfig{VcpuCount: tierCfg.VCPUCount, MemSizeMib: tierCfg.MemSizeMiB, Smt: false},
		firecracker.BootSource{KernelImagePath: tmpl.KernelImagePath, BootArgs: firecracker.KernelArgs},
		firecracker.Drive{DriveID: "rootfs", PathOnHost: tmpl.RootfsPath, IsRootDevice: true, IsReadOnly: true},
		firecracker.VsockDevice{GuestCID: 3, UDSPath: vsockPath},
	); err != nil {
		return fmt.Errorf("configure microvm: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start microvm: %w", err)
	}

	// 等待 Guest Agent 就绪
	s := &session.Session{
		ID:        "builder-" + tmpl.ID,
		Tier:      tmpl.Tier,
		VsockPath: vsockPath,
		AgentPort: b.guestAgentPort,
		CreatedAt: time.Now(),
	}
	bootTimeout := time.Duration(b.bootTimeoutSeconds) * time.Second
	if err := session.WaitForAgent(ctx, s, bootTimeout); err != nil {
		return fmt.Errorf("guest agent not ready: %w", err)
	}

	// 暂停 VM 后打快照
	if err := client.Pause(ctx); err != nil {
		return fmt.Errorf("pause vm: %w", err)
	}

	snapPath := filepath.Join(buildDir, "disk.snap")
	memPath := filepath.Join(buildDir, "mem.snap")
	if err := client.CreateSnapshot(ctx, snapPath, memPath); err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	// 上传到 MinIO（mem 在前，disk 在后，与 store.Upload 参数顺序一致）
	if err := b.store.Upload(ctx, tmpl.ID, memPath, snapPath); err != nil {
		return fmt.Errorf("upload snapshot: %w", err)
	}

	b.logger.Info("snapshot built", logging.LogFields{}, map[string]any{"template_id": tmpl.ID})
	return nil
}

// EnsureAll 遍历 registry 中所有 template，对 MinIO 上不存在快照的 template 调用 Build。
// 用于服务启动时的阻塞检查，确保 warm pool 有快照可用。
func (b *Builder) EnsureAll(ctx context.Context, registry *template.Registry) error {
	for _, tmpl := range registry.All() {
		exists, err := b.store.Exists(ctx, tmpl.ID)
		if err != nil {
			return fmt.Errorf("check snapshot for %q: %w", tmpl.ID, err)
		}
		if exists {
			b.logger.Info("snapshot exists, skipping", logging.LogFields{}, map[string]any{"template_id": tmpl.ID})
			continue
		}
		if err := b.Build(ctx, tmpl); err != nil {
			return fmt.Errorf("build snapshot for %q: %w", tmpl.ID, err)
		}
	}
	return nil
}

// startProcess 以非阻塞方式启动 Firecracker 进程，返回 os.Process。
func startProcess(firecrackerBin, apiSocket string) (*os.Process, error) {
	cmd := exec.Command(firecrackerBin, "--api-sock", apiSocket, "--log-level", "Error")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}
