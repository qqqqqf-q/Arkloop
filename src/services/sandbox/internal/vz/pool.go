//go:build darwin

package vz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"

	vz "github.com/Code-Hex/vz/v3"
)

// Config holds all parameters required by the Vz pool.
type Config struct {
	WarmSizes             map[string]int
	RefillIntervalSeconds int
	MaxRefillConcurrency  int
	KernelImagePath       string
	InitrdPath            string // optional initramfs for loading kernel modules
	RootfsPath            string
	SocketBaseDir         string
	BootTimeoutSeconds    int
	GuestAgentPort        uint32
	Logger                *logging.JSONLogger
}

type entry struct {
	session *session.Session
	vm      *vz.VirtualMachine
}

// Pool manages macOS Virtualization.framework VMs as sandbox execution
// environments. It follows the same warm-pool pattern used by the Docker
// and Firecracker providers.
type Pool struct {
	cfg   Config
	ready map[string]chan *entry
	sem   chan struct{}
	stop  chan struct{}
	wg    sync.WaitGroup

	totalCreated   atomic.Int64
	totalDestroyed atomic.Int64

	mu     sync.Mutex
	active map[string]*entry
}

// New creates a Pool instance without starting background goroutines.
func New(cfg Config) *Pool {
	ready := make(map[string]chan *entry)
	for tier, size := range cfg.WarmSizes {
		if size > 0 {
			ready[tier] = make(chan *entry, size)
		}
	}
	return &Pool{
		cfg:    cfg,
		ready:  ready,
		sem:    make(chan struct{}, cfg.MaxRefillConcurrency),
		stop:   make(chan struct{}),
		active: make(map[string]*entry),
	}
}

// Start launches background refiller goroutines for each configured tier.
func (p *Pool) Start() {
	for tier, target := range p.cfg.WarmSizes {
		if target <= 0 {
			continue
		}
		p.wg.Add(1)
		go p.refiller(tier, target)
	}
}

// Acquire returns a ready VM session. It tries the warm pool first and
// falls back to on-demand creation.
func (p *Pool) Acquire(ctx context.Context, tier string) (*session.Session, *os.Process, error) {
	var e *entry
	if ch, ok := p.ready[tier]; ok {
		select {
		case e = <-ch:
		default:
		}
	}
	if e == nil {
		var err error
		e, err = p.createVM(ctx, tier)
		if err != nil {
			return nil, nil, err
		}
	}

	p.mu.Lock()
	p.active[e.session.SocketDir] = e
	p.mu.Unlock()

	return e.session, nil, nil
}

// Destroy tears down the VM associated with sessionID.
func (p *Pool) DestroyVM(_ *os.Process, socketDir string) {
	p.mu.Lock()
	var found *entry
	var foundID string
	for id, e := range p.active {
		if e.session.SocketDir == socketDir {
			found = e
			foundID = id
			break
		}
	}
	if found != nil {
		delete(p.active, foundID)
	}
	p.mu.Unlock()

	if found != nil {
		p.destroyResources(found.vm, socketDir)
	} else {
		p.totalDestroyed.Add(1)
	}
}

// Ready reports whether every enabled tier has reached its warm target.
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

// Stats returns current pool statistics.
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

// Drain stops all refillers, drains warm channels, and destroys all active VMs.
func (p *Pool) Drain(ctx context.Context) {
	close(p.stop)
	p.wg.Wait()
	for _, ch := range p.ready {
		p.drainChannel(ch)
	}

	p.mu.Lock()
	remaining := make(map[string]*entry, len(p.active))
	for id, e := range p.active {
		remaining[id] = e
	}
	p.active = make(map[string]*entry)
	p.mu.Unlock()

	for _, e := range remaining {
		p.destroyResources(e.vm, e.session.SocketDir)
	}
}

// ---------------------------------------------------------------------------
// VM lifecycle
// ---------------------------------------------------------------------------

func (p *Pool) createVM(ctx context.Context, tier string) (*entry, error) {
	res := resourcesFor(tier)
	suffix := generateID()
	socketDir := filepath.Join(p.cfg.SocketBaseDir, suffix)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(socketDir) }

	// Each VM needs a writable copy of the rootfs image.
	vmRootfs := filepath.Join(socketDir, "rootfs.ext4")
	if err := copyFile(p.cfg.RootfsPath, vmRootfs); err != nil {
		cleanup()
		return nil, fmt.Errorf("copy rootfs: %w", err)
	}

	// --- Boot loader ---
	bootOpts := []vz.LinuxBootLoaderOption{
		vz.WithCommandLine("console=hvc0 root=/dev/vda rw"),
	}
	if p.cfg.InitrdPath != "" {
		bootOpts = append(bootOpts, vz.WithInitrd(p.cfg.InitrdPath))
	}
	bootLoader, err := vz.NewLinuxBootLoader(
		p.cfg.KernelImagePath,
		bootOpts...,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create boot loader: %w", err)
	}

	// --- VM configuration ---
	vmCfg, err := vz.NewVirtualMachineConfiguration(
		bootLoader,
		res.CPUCount,
		res.MemoryMiB*1024*1024,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create vm config: %w", err)
	}

	// Block storage (rootfs)
	diskAttachment, err := vz.NewDiskImageStorageDeviceAttachment(vmRootfs, false)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create disk attachment: %w", err)
	}
	blockDev, err := vz.NewVirtioBlockDeviceConfiguration(diskAttachment)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create block device: %w", err)
	}
	vmCfg.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{blockDev})

	// Network (NAT)
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create nat attachment: %w", err)
	}
	netDev, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create network device: %w", err)
	}
	vmCfg.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{netDev})

	// Vsock
	vsockDev, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create vsock device: %w", err)
	}
	vmCfg.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{vsockDev})

	// Serial console (discard output)
	serialIn, err := os.Open(os.DevNull)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("open devnull for serial in: %w", err)
	}
	serialOut, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		serialIn.Close()
		cleanup()
		return nil, fmt.Errorf("open devnull for serial out: %w", err)
	}
	serialAttachment, err := vz.NewFileHandleSerialPortAttachment(serialIn, serialOut)
	if err != nil {
		serialIn.Close()
		serialOut.Close()
		cleanup()
		return nil, fmt.Errorf("create serial attachment: %w", err)
	}
	consoleDev, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		serialIn.Close()
		serialOut.Close()
		cleanup()
		return nil, fmt.Errorf("create console device: %w", err)
	}
	vmCfg.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{consoleDev})

	// Validate
	validated, err := vmCfg.Validate()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("validate vm config: %w", err)
	}
	if !validated {
		cleanup()
		return nil, fmt.Errorf("vm config validation failed")
	}

	// --- Create and start VM ---
	vm, err := vz.NewVirtualMachine(vmCfg)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create vm: %w", err)
	}

	if err := vm.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("start vm: %w", err)
	}

	// Wait for the VM to reach the Running state.
	bootTimeout := time.Duration(p.cfg.BootTimeoutSeconds) * time.Second
	if bootTimeout <= 0 {
		bootTimeout = 30 * time.Second
	}
	if err := waitForVMState(ctx, vm, vz.VirtualMachineStateRunning, bootTimeout); err != nil {
		stopVM(vm)
		cleanup()
		return nil, fmt.Errorf("vm not running: %w", err)
	}

	// --- Dialer via vsock ---
	s := &session.Session{
		Tier:      tier,
		Dial:      newVzVsockDialer(vm, p.cfg.GuestAgentPort),
		CreatedAt: time.Now(),
		SocketDir: socketDir,
	}

	if err := session.WaitForAgent(ctx, s, bootTimeout); err != nil {
		stopVM(vm)
		cleanup()
		return nil, fmt.Errorf("vz agent not ready: %w", err)
	}

	p.totalCreated.Add(1)
	p.cfg.Logger.Info("vz vm created", logging.LogFields{}, map[string]any{
		"tier": tier, "id": suffix,
	})
	return &entry{session: s, vm: vm}, nil
}

func (p *Pool) destroyResources(vm *vz.VirtualMachine, socketDir string) {
	if vm != nil {
		stopVM(vm)
	}
	if socketDir != "" {
		_ = os.RemoveAll(socketDir)
	}
	p.totalDestroyed.Add(1)
}

func (p *Pool) destroyEntry(e *entry) {
	if e == nil {
		return
	}
	p.destroyResources(e.vm, e.session.SocketDir)
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

// ---------------------------------------------------------------------------
// Refiller goroutines
// ---------------------------------------------------------------------------

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
		e, err := p.createVM(ctx, tier)
		cancel()
		<-p.sem

		if err != nil {
			p.cfg.Logger.Warn("vz pool refill failed", logging.LogFields{},
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

// ---------------------------------------------------------------------------
// Vsock dialer and net.Conn adapter
// ---------------------------------------------------------------------------

func newVzVsockDialer(vm *vz.VirtualMachine, port uint32) session.Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		socketDevices := vm.SocketDevices()
		if len(socketDevices) == 0 {
			return nil, fmt.Errorf("no vsock device found")
		}
		conn, err := socketDevices[0].Connect(port)
		if err != nil {
			return nil, fmt.Errorf("vsock connect port %d: %w", port, err)
		}
		return &vzConn{conn: conn}, nil
	}
}

// vzConn adapts a VirtioSocketConnection to the net.Conn interface.
type vzConn struct {
	conn *vz.VirtioSocketConnection
}

func (c *vzConn) Read(b []byte) (int, error)  { return c.conn.Read(b) }
func (c *vzConn) Write(b []byte) (int, error) { return c.conn.Write(b) }
func (c *vzConn) Close() error                { return c.conn.Close() }

func (c *vzConn) LocalAddr() net.Addr  { return vsockAddr{label: "local"} }
func (c *vzConn) RemoteAddr() net.Addr { return vsockAddr{label: "remote"} }

func (c *vzConn) SetDeadline(t time.Time) error      { return nil }
func (c *vzConn) SetReadDeadline(t time.Time) error   { return nil }
func (c *vzConn) SetWriteDeadline(t time.Time) error  { return nil }

// vsockAddr is a minimal net.Addr for vzConn.
type vsockAddr struct{ label string }

func (a vsockAddr) Network() string { return "vsock" }
func (a vsockAddr) String() string  { return "vz-" + a.label }

// ---------------------------------------------------------------------------
// VM helpers
// ---------------------------------------------------------------------------

// stopVM attempts a graceful RequestStop, then forces Stop after a timeout.
func stopVM(vm *vz.VirtualMachine) {
	if vm.State() == vz.VirtualMachineStateStopped ||
		vm.State() == vz.VirtualMachineStateError {
		return
	}

	if vm.CanRequestStop() {
		_, _ = vm.RequestStop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = waitForVMState(ctx, vm, vz.VirtualMachineStateStopped, 5*time.Second)
		if vm.State() == vz.VirtualMachineStateStopped {
			return
		}
	}

	if vm.CanStop() {
		_ = vm.Stop()
	}
}

// waitForVMState polls until the VM reaches the desired state or the timeout
// expires.
func waitForVMState(ctx context.Context, vm *vz.VirtualMachine, desired vz.VirtualMachineState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if vm.State() == desired {
			return nil
		}
		if vm.State() == vz.VirtualMachineStateError {
			return fmt.Errorf("vm entered error state")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("vm did not reach state %v within %s", desired, timeout)
}

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

// copyFile is implemented in copyfile_darwin.go (uses clonefileat on macOS).

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "vz-" + hex.EncodeToString(b)
}
