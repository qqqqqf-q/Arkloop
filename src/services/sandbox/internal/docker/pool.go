package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
)

const defaultAgentNetworkName = "arkloop_sandbox_agent"

// Config 持有 DockerPool 运行所需的参数。
type Config struct {
	WarmSizes             map[string]int
	RefillIntervalSeconds int
	MaxRefillConcurrency  int

	Image          string // sandbox-agent 容器镜像
	AllowEgress    bool   // 允许派生容器访问外网
	NetworkName    string // agent 容器加入的 Docker 网络；非空时通过容器 IP 连接
	GuestAgentPort uint32
	SocketBaseDir  string // 用于存放临时文件的目录
	Logger         *logging.JSONLogger
}

type entry struct {
	session     *session.Session
	containerID string
}

// Pool 通过 Docker Engine API 管理容器化的 sandbox 执行环境。
type Pool struct {
	cfg   Config
	cli   *client.Client
	ready map[string]chan *entry
	sem   chan struct{}
	stop  chan struct{}
	wg    sync.WaitGroup

	totalCreated   atomic.Int64
	totalDestroyed atomic.Int64

	// containerID -> session 映射，用于 DestroyVM
	mu         sync.Mutex
	containers map[string]string // socketDir -> containerID
}

// New 创建 DockerPool 实例。
func New(cfg Config) (*Pool, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	// 验证 Docker daemon 可达
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}

	networkName := strings.TrimSpace(cfg.NetworkName)
	if networkName == "" {
		networkName = defaultAgentNetworkName
	}
	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ensureCancel()
	if err := ensureNetworkExists(ensureCtx, cli, networkName, !cfg.AllowEgress); err != nil {
		cli.Close()
		return nil, err
	}

	ready := make(map[string]chan *entry)
	for tier, size := range cfg.WarmSizes {
		if size > 0 {
			ready[tier] = make(chan *entry, size)
		}
	}

	return &Pool{
		cfg:        cfg,
		cli:        cli,
		ready:      ready,
		sem:        make(chan struct{}, cfg.MaxRefillConcurrency),
		stop:       make(chan struct{}),
		containers: make(map[string]string),
	}, nil
}

func ensureNetworkExists(ctx context.Context, cli *client.Client, name string, internal bool) error {
	inspected, err := cli.NetworkInspect(ctx, name, network.InspectOptions{})
	if err == nil {
		if inspected.Internal != internal {
			return fmt.Errorf("docker network %s internal=%t, expected internal=%t; recreate the network or change ARKLOOP_SANDBOX_DOCKER_ALLOW_EGRESS", name, inspected.Internal, internal)
		}
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect docker network %s: %w", name, err)
	}

	_, err = cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: internal,
		Labels: map[string]string{
			"arkloop.role": "sandbox-agent-network",
		},
	})
	if err != nil {
		return fmt.Errorf("create docker network %s: %w", name, err)
	}
	return nil
}

// Start 启动所有 tier 的后台 refiller goroutine。
func (p *Pool) Start() {
	for tier, target := range p.cfg.WarmSizes {
		if target <= 0 {
			continue
		}
		p.wg.Add(1)
		go p.refiller(tier, target)
	}
}

// Acquire 获取一个就绪的容器。优先从 warm pool 取，pool 为空时按需创建。
func (p *Pool) Acquire(ctx context.Context, tier string) (*session.Session, *os.Process, error) {
	if ch, ok := p.ready[tier]; ok {
		select {
		case e := <-ch:
			return e.session, nil, nil
		default:
		}
	}
	e, err := p.createContainer(ctx, tier)
	if err != nil {
		return nil, nil, err
	}
	return e.session, nil, nil
}

// DestroyVM 销毁一个容器。proc 参数在 Docker 后端中不使用。
func (p *Pool) DestroyVM(proc *os.Process, socketDir string) {
	p.mu.Lock()
	containerID, ok := p.containers[socketDir]
	if ok {
		delete(p.containers, socketDir)
	}
	p.mu.Unlock()

	if ok && containerID != "" {
		rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = p.cli.ContainerRemove(rmCtx, containerID, container.RemoveOptions{Force: true})
	}

	if socketDir != "" {
		_ = os.RemoveAll(socketDir)
	}
	p.totalDestroyed.Add(1)
}

// Ready 返回所有启用了预热的 tier 是否已达到目标数量。
func (p *Pool) Ready() bool {
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
func (p *Pool) Stats() session.PoolStats {
	readyByTier := make(map[string]int)
	targetByTier := make(map[string]int)
	for tier, target := range p.cfg.WarmSizes {
		targetByTier[tier] = target
		if ch, ok := p.ready[tier]; ok {
			readyByTier[tier] = len(ch)
		}
	}
	return session.PoolStats{
		ReadyByTier:    readyByTier,
		TargetByTier:   targetByTier,
		TotalCreated:   p.totalCreated.Load(),
		TotalDestroyed: p.totalDestroyed.Load(),
	}
}

// Drain 停止所有 refiller 并销毁所有预热容器。
func (p *Pool) Drain(ctx context.Context) {
	close(p.stop)
	p.wg.Wait()
	for _, ch := range p.ready {
		p.drainChannel(ch)
	}
	_ = p.cli.Close()
}

func (p *Pool) drainChannel(ch chan *entry) {
	for {
		select {
		case e := <-ch:
			p.destroyEntry(e)
		default:
			return
		}
	}
}

func (p *Pool) destroyEntry(e *entry) {
	if e == nil {
		return
	}
	p.DestroyVM(nil, e.session.SocketDir)
}

func (p *Pool) refiller(tier string, target int) {
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

func (p *Pool) fillTier(tier string, target int) {
	ch := p.ready[tier]
	for len(ch) < target {
		select {
		case <-p.stop:
			return
		case p.sem <- struct{}{}:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		e, err := p.createContainer(ctx, tier)
		cancel()
		<-p.sem

		if err != nil {
			p.cfg.Logger.Warn("docker pool refill failed", logging.LogFields{},
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

type createPlan struct {
	containerCfg      *container.Config
	hostCfg           *container.HostConfig
	exposedPort       nat.Port
	agentPort         string
	dialByContainerIP bool
	attachNetworkName string
}

func buildCreatePlan(cfg Config, tier string) createPlan {
	res := resourcesFor(tier)
	agentPort := strconv.FormatUint(uint64(cfg.GuestAgentPort), 10)
	exposedPort := nat.Port(agentPort + "/tcp")

	dialByContainerIP := cfg.NetworkName != ""
	attachNetworkName := cfg.NetworkName
	if attachNetworkName == "" {
		attachNetworkName = defaultAgentNetworkName
	}

	containerCfg := &container.Config{
		Image: cfg.Image,
		User:  "1000:1000",
		Env: []string{
			"SANDBOX_AGENT_LISTEN=tcp",
		},
		ExposedPorts: nat.PortSet{
			exposedPort: struct{}{},
		},
		Labels: map[string]string{
			"com.docker.compose.project": "arkloop",
			"com.docker.compose.service": "sandbox-agent",
			"arkloop.tier":               tier,
		},
	}

	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			NanoCPUs:  res.NanoCPUs,
			Memory:    res.MemoryMB * 1024 * 1024,
			PidsLimit: ptrInt64(256),
		},
		NetworkMode:    container.NetworkMode(attachNetworkName),
		AutoRemove:     false,
		ReadonlyRootfs: false,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
	}

	if !dialByContainerIP {
		// 无指定网络时，使用端口映射（本地开发/Linux host 网络）
		hostCfg.PortBindings = nat.PortMap{
			exposedPort: []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: "0"},
			},
		}
	}

	return createPlan{
		containerCfg:      containerCfg,
		hostCfg:           hostCfg,
		exposedPort:       exposedPort,
		agentPort:         agentPort,
		dialByContainerIP: dialByContainerIP,
		attachNetworkName: attachNetworkName,
	}
}

// createContainer 创建并启动一个 sandbox 容器，返回就绪的 entry。
func (p *Pool) createContainer(ctx context.Context, tier string) (*entry, error) {
	id := generateID()

	socketDir := fmt.Sprintf("%s/%s", p.cfg.SocketBaseDir, id)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(socketDir)
	}

	plan := buildCreatePlan(p.cfg, tier)
	resp, err := p.cli.ContainerCreate(ctx, plan.containerCfg, plan.hostCfg, nil, nil, "sandbox-"+id)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("docker create: %w", err)
	}
	containerID := resp.ID

	destroyContainer := func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = p.cli.ContainerRemove(rmCtx, containerID, container.RemoveOptions{Force: true})
		cleanup()
	}

	if err := p.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		destroyContainer()
		return nil, fmt.Errorf("docker start: %w", err)
	}

	// 获取 agent 地址
	inspect, err := p.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		destroyContainer()
		return nil, fmt.Errorf("docker inspect: %w", err)
	}

	var agentAddr string
	if plan.dialByContainerIP {
		epSettings, ok := inspect.NetworkSettings.Networks[plan.attachNetworkName]
		if !ok || epSettings.IPAddress == "" {
			destroyContainer()
			return nil, fmt.Errorf("container has no IP in network %s", plan.attachNetworkName)
		}
		agentAddr = net.JoinHostPort(epSettings.IPAddress, plan.agentPort)
	} else {
		bindings, ok := inspect.NetworkSettings.Ports[plan.exposedPort]
		if !ok || len(bindings) == 0 {
			destroyContainer()
			return nil, fmt.Errorf("no port binding for %s", plan.exposedPort)
		}
		agentAddr = net.JoinHostPort(bindings[0].HostIP, bindings[0].HostPort)
	}

	// 注册 containerID 映射
	p.mu.Lock()
	p.containers[socketDir] = containerID
	p.mu.Unlock()

	s := &session.Session{
		ID:        id,
		Tier:      tier,
		Dial:      session.NewTCPDialer(agentAddr),
		CreatedAt: time.Now(),
		SocketDir: socketDir,
	}

	// 等待 agent 就绪
	const agentReadyTimeout = 30 * time.Second
	if err := session.WaitForAgent(ctx, s, agentReadyTimeout); err != nil {
		destroyContainer()
		return nil, fmt.Errorf("docker agent not ready: %w", err)
	}

	p.totalCreated.Add(1)
	return &entry{session: s, containerID: containerID}, nil
}

// EnsureImage 确保 sandbox 镜像存在于本地。
func (p *Pool) EnsureImage(ctx context.Context) error {
	_, _, err := p.cli.ImageInspectWithRaw(ctx, p.cfg.Image)
	if err == nil {
		return nil
	}

	p.cfg.Logger.Info("pulling sandbox image", logging.LogFields{},
		map[string]any{"image": p.cfg.Image})

	reader, err := p.cli.ImagePull(ctx, p.cfg.Image, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", p.cfg.Image, err)
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "dock-" + hex.EncodeToString(b)
}

func ptrInt64(v int64) *int64 {
	return &v
}
