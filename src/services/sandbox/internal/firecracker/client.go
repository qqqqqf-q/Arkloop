package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Client 是 Firecracker HTTP API 的轻量封装，通过 Unix domain socket 通信。
type Client struct {
	http *http.Client
}

// NewClient 创建绑定到指定 API socket 路径的客户端。
// 不设全局 Timeout，由每个请求的 ctx deadline 控制超时。
func NewClient(apiSocketPath string) *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", apiSocketPath)
				},
			},
		},
	}
}

// MachineConfig 对应 Firecracker PUT /machine-config。
type MachineConfig struct {
	VcpuCount  int64 `json:"vcpu_count"`
	MemSizeMib int64 `json:"mem_size_mib"`
	Smt        bool  `json:"smt"`
}

// BootSource 对应 Firecracker PUT /boot-source。
type BootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

// Drive 对应 Firecracker PUT /drives/{drive_id}。
type Drive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

// VsockDevice 对应 Firecracker PUT /vsock。
type VsockDevice struct {
	GuestCID uint32 `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

type NetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	HostDevName string `json:"host_dev_name"`
	GuestMAC    string `json:"guest_mac,omitempty"`
}

type NetworkInterfaceUpdate struct {
	HostDevName string `json:"host_dev_name"`
}

// Configure 依次设置 machine-config、boot-source、rootfs drive、vsock 设备。
func (c *Client) Configure(ctx context.Context, mc MachineConfig, bs BootSource, drv Drive, vsock VsockDevice, network *NetworkInterface) error {
	if err := c.put(ctx, "/machine-config", mc); err != nil {
		return fmt.Errorf("machine-config: %w", err)
	}
	if err := c.put(ctx, "/boot-source", bs); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}
	if err := c.put(ctx, "/drives/"+drv.DriveID, drv); err != nil {
		return fmt.Errorf("drives/%s: %w", drv.DriveID, err)
	}
	if err := c.put(ctx, "/vsock", vsock); err != nil {
		return fmt.Errorf("vsock: %w", err)
	}
	if network != nil {
		if err := c.put(ctx, "/network-interfaces/"+network.IfaceID, network); err != nil {
			return fmt.Errorf("network-interfaces/%s: %w", network.IfaceID, err)
		}
	}
	return nil
}

// Start 向 Firecracker 发送 InstanceStart 动作，启动 microVM。
func (c *Client) Start(ctx context.Context) error {
	body := map[string]string{"action_type": "InstanceStart"}
	if err := c.put(ctx, "/actions", body); err != nil {
		return fmt.Errorf("InstanceStart: %w", err)
	}
	return nil
}

// Pause 暂停 microVM（打快照前必须暂停）。
func (c *Client) Pause(ctx context.Context) error {
	if err := c.patch(ctx, "/vm", map[string]string{"state": "Paused"}); err != nil {
		return fmt.Errorf("pause vm: %w", err)
	}
	return nil
}

// Resume 恢复 microVM 运行。
func (c *Client) Resume(ctx context.Context) error {
	if err := c.patch(ctx, "/vm", map[string]string{"state": "Resumed"}); err != nil {
		return fmt.Errorf("resume vm: %w", err)
	}
	return nil
}

// createSnapshotRequest 对应 Firecracker PUT /snapshot/create。
type createSnapshotRequest struct {
	SnapshotType string `json:"snapshot_type"`
	SnapshotPath string `json:"snapshot_path"`
	MemFilePath  string `json:"mem_file_path"`
}

// CreateSnapshot 创建全量快照，snapshotPath 和 memFilePath 均为宿主机上的本地路径。
func (c *Client) CreateSnapshot(ctx context.Context, snapshotPath, memFilePath string) error {
	req := createSnapshotRequest{
		SnapshotType: "Full",
		SnapshotPath: snapshotPath,
		MemFilePath:  memFilePath,
	}
	if err := c.put(ctx, "/snapshot/create", req); err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	return nil
}

// memBackend 描述快照内存文件的后端类型，当前仅支持 File 模式。
type memBackend struct {
	BackendType string `json:"backend_type"` // "File"
	BackendPath string `json:"backend_path"`
}

// loadSnapshotRequest 对应 Firecracker PUT /snapshot/load。
type loadSnapshotRequest struct {
	SnapshotPath        string     `json:"snapshot_path"`
	MemBackend          memBackend `json:"mem_backend"`
	EnableDiffSnapshots bool       `json:"enable_diff_snapshots"`
	ResumeVM            bool       `json:"resume_vm"`
}

// LoadSnapshot 从快照恢复 microVM。resumeVM 为 true 时加载后立即恢复运行。
func (c *Client) LoadSnapshot(ctx context.Context, snapshotPath, memBackendPath string, resumeVM bool) error {
	req := loadSnapshotRequest{
		SnapshotPath:        snapshotPath,
		MemBackend:          memBackend{BackendType: "File", BackendPath: memBackendPath},
		EnableDiffSnapshots: false,
		ResumeVM:            resumeVM,
	}
	if err := c.put(ctx, "/snapshot/load", req); err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	return nil
}

func (c *Client) UpdateNetworkInterface(ctx context.Context, ifaceID string, update NetworkInterfaceUpdate) error {
	if err := c.patch(ctx, "/network-interfaces/"+ifaceID, update); err != nil {
		return fmt.Errorf("update network interface %s: %w", ifaceID, err)
	}
	return nil
}

func (c *Client) put(ctx context.Context, path string, body any) error {
	return c.doJSON(ctx, http.MethodPut, path, body)
}

func (c *Client) patch(ctx context.Context, path string, body any) error {
	return c.doJSON(ctx, http.MethodPatch, path, body)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	return nil
}
