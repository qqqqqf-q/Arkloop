// Guest Agent — 编译后置于 microVM rootfs 内 /usr/local/bin/sandbox-agent
//
// 构建命令（在宿主机上交叉编译为静态二进制）：
//
//	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sandbox-agent ./agent
//
// rootfs 启动时通过 init 系统（OpenRC/busybox init/systemd）自动运行本程序。
// 也可配置为 kernel 的 init 进程：
//
//	kernel_args: "... init=/usr/local/bin/sandbox-agent"
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net"
	nethttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/sandbox/internal/environment"
	environmentcontract "arkloop/services/sandbox/internal/environment/contract"
	shellapi "arkloop/services/sandbox/internal/shell"

	"github.com/mdlayher/vsock"
)

const (
	listenPort     = 8080
	maxCodeBytes   = 4 * 1024 * 1024 // 4 MB
	maxStdoutBytes = 64 * 1024
	maxStderrBytes = 64 * 1024

	artifactOutputDir = "/tmp/output"
	maxArtifactFiles  = 5
	maxArtifactBytes  = 5 * 1024 * 1024  // 单文件 5 MB
	maxTotalArtifacts = 10 * 1024 * 1024 // 总上限 10 MB
)

type limitedBuffer struct {
	limit int
	buf   []byte
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit < 0 {
		limit = 0
	}
	return &limitedBuffer{
		limit: limit,
		buf:   make([]byte, 0, minInt(limit, 1024)),
	}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	if len(b.buf) >= b.limit {
		return len(p), nil
	}
	remaining := b.limit - len(b.buf)
	if len(p) <= remaining {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	b.buf = append(b.buf, p[:remaining]...)
	return len(p), nil
}

func (b *limitedBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

func (b *limitedBuffer) Len() int {
	return len(b.buf)
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type ExecJob struct {
	Language  string `json:"language"` // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}

type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// AgentRequest 是 v2 协议的请求格式，通过 action 字段区分操作类型。
type AgentRequest struct {
	Action      string                            `json:"action"`
	ExecJob     *ExecJob                          `json:"exec_job,omitempty"`
	ExecCommand *shellapi.AgentExecCommandRequest `json:"exec_command,omitempty"`
	WriteStdin  *shellapi.AgentWriteStdinRequest  `json:"write_stdin,omitempty"`
	Checkpoint  *shellapi.AgentCheckpointRequest  `json:"checkpoint,omitempty"`
	Environment *EnvironmentRequest               `json:"environment,omitempty"`
	Network     *GuestNetworkRequest              `json:"network,omitempty"`
}

// AgentResponse 是 v2 协议的统一响应。
type AgentResponse struct {
	Action      string                            `json:"action"`
	Exec        *ExecResult                       `json:"exec,omitempty"`
	Artifacts   *FetchArtifactsResult             `json:"artifacts,omitempty"`
	Environment *EnvironmentResponse              `json:"environment,omitempty"`
	Session     *shellapi.AgentSessionResponse    `json:"session,omitempty"`
	Debug       *shellapi.AgentDebugResponse      `json:"debug,omitempty"`
	Checkpoint  *shellapi.AgentCheckpointResponse `json:"checkpoint,omitempty"`
	Code        string                            `json:"code,omitempty"`
	Error       string                            `json:"error,omitempty"`
}

type EnvironmentRequest struct {
	Scope    string                        `json:"scope"`
	Archive  string                        `json:"archive,omitempty"`
	Subtrees []string                      `json:"subtrees,omitempty"`
	Paths    []string                      `json:"paths,omitempty"`
	Manifest *environmentcontract.Manifest `json:"manifest,omitempty"`
	Files    []environment.FilePayload     `json:"files,omitempty"`
	Reset    bool                          `json:"reset,omitempty"`
}

type EnvironmentResponse struct {
	Archive  string                        `json:"archive,omitempty"`
	Manifest *environmentcontract.Manifest `json:"manifest,omitempty"`
	Files    []environment.FilePayload     `json:"files,omitempty"`
}

type ArtifactEntry struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

type FetchArtifactsResult struct {
	Artifacts []ArtifactEntry `json:"artifacts"`
	Truncated bool            `json:"truncated"`
}

var shellController = NewShellController()

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var l net.Listener
	var err error

	if os.Getenv("SANDBOX_AGENT_LISTEN") == "tcp" {
		l, err = net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
		if err != nil {
			return fmt.Errorf("tcp listen :%d: %w", listenPort, err)
		}
		fmt.Fprintf(os.Stderr, "sandbox-agent listening on tcp port %d\n", listenPort)
	} else {
		l, err = vsock.Listen(listenPort, nil)
		if err != nil {
			return fmt.Errorf("vsock listen :%d: %w", listenPort, err)
		}
		fmt.Fprintf(os.Stderr, "sandbox-agent listening on vsock port %d\n", listenPort)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			// listener 关闭时退出
			return nil
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	// 读取原始 JSON 以判断协议版本
	var raw json.RawMessage
	if err := json.NewDecoder(conn).Decode(&raw); err != nil {
		writeJSON(conn, ExecResult{Stderr: fmt.Sprintf("decode request: %v", err), ExitCode: 1})
		return
	}

	// 尝试解析为 v2 协议（含 action 字段）
	var req AgentRequest
	if err := json.Unmarshal(raw, &req); err == nil && req.Action != "" {
		handleV2(conn, req)
		return
	}

	// 回退到 v1 协议：直接作为 ExecJob 处理
	var job ExecJob
	if err := json.Unmarshal(raw, &job); err != nil {
		writeJSON(conn, ExecResult{Stderr: fmt.Sprintf("decode job: %v", err), ExitCode: 1})
		return
	}
	ensureOutputDir()
	result := executeJob(job)
	writeJSON(conn, result)
}

func handleV2(conn net.Conn, req AgentRequest) {
	switch req.Action {
	case "exec":
		if req.ExecJob == nil {
			writeJSON(conn, AgentResponse{Action: "exec", Error: "exec_job is required"})
			return
		}
		ensureOutputDir()
		result := executeJob(*req.ExecJob)
		writeJSON(conn, AgentResponse{Action: "exec", Exec: &result})

	case "fetch_artifacts":
		result := fetchArtifacts()
		writeJSON(conn, AgentResponse{Action: "fetch_artifacts", Artifacts: &result})

	case "exec_command":
		result, code, errMsg := shellController.ExecCommand(derefExecCommand(req.ExecCommand))
		writeJSON(conn, AgentResponse{Action: req.Action, Session: result, Code: code, Error: errMsg})

	case "write_stdin":
		result, code, errMsg := shellController.WriteStdin(derefWriteStdin(req.WriteStdin))
		writeJSON(conn, AgentResponse{Action: req.Action, Session: result, Code: code, Error: errMsg})

	case "shell_debug_snapshot":
		result, code, errMsg := shellController.DebugSnapshot()
		writeJSON(conn, AgentResponse{Action: req.Action, Debug: result, Code: code, Error: errMsg})

	case "shell_checkpoint_export":
		result, code, errMsg := shellController.CheckpointExport()
		writeJSON(conn, AgentResponse{Action: req.Action, Checkpoint: result, Code: code, Error: errMsg})

	case "shell_restore_import":
		result, code, errMsg := shellController.RestoreImport(derefCheckpoint(req.Checkpoint))
		writeJSON(conn, AgentResponse{Action: req.Action, Checkpoint: result, Code: code, Error: errMsg})

	case "environment_export":
		if req.Environment == nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: "environment is required"})
			return
		}
		archive, err := exportEnvironmentArchive(strings.TrimSpace(req.Environment.Scope))
		if err != nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: err.Error()})
			return
		}
		writeJSON(conn, AgentResponse{Action: req.Action, Environment: &EnvironmentResponse{Archive: base64.StdEncoding.EncodeToString(archive)}})

	case "environment_import":
		if req.Environment == nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: "environment is required"})
			return
		}
		archive, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Environment.Archive))
		if err != nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: fmt.Sprintf("decode environment archive: %v", err)})
			return
		}
		if err := importEnvironmentArchive(strings.TrimSpace(req.Environment.Scope), archive); err != nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: err.Error()})
			return
		}
		writeJSON(conn, AgentResponse{Action: req.Action, Environment: &EnvironmentResponse{}})

	case "configure_guest_network":
		if req.Network == nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: "network is required"})
			return
		}
		if err := configureGuestNetwork(*req.Network); err != nil {
			writeJSON(conn, AgentResponse{Action: req.Action, Error: err.Error()})
			return
		}
		writeJSON(conn, AgentResponse{Action: req.Action})

	default:
		writeJSON(conn, AgentResponse{Action: req.Action, Error: fmt.Sprintf("unknown action: %s", req.Action)})
	}
}

func derefExecCommand(req *shellapi.AgentExecCommandRequest) shellapi.AgentExecCommandRequest {
	if req == nil {
		return shellapi.AgentExecCommandRequest{}
	}
	return *req
}

func derefWriteStdin(req *shellapi.AgentWriteStdinRequest) shellapi.AgentWriteStdinRequest {
	if req == nil {
		return shellapi.AgentWriteStdinRequest{}
	}
	return *req
}

func derefCheckpoint(req *shellapi.AgentCheckpointRequest) shellapi.AgentCheckpointRequest {
	if req == nil {
		return shellapi.AgentCheckpointRequest{}
	}
	return *req
}

func ensureOutputDir() {
	_ = ensureWorkloadBaseDirs()
}

func executeJob(job ExecJob) ExecResult {
	if len(job.Code) > maxCodeBytes {
		return ExecResult{Stderr: "code too large", ExitCode: 1}
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > 5*time.Minute {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	switch job.Language {
	case "python":
		cmd = buildPythonCmd(ctx, job.Code)
	case "shell":
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", job.Code)
	default:
		return ExecResult{Stderr: fmt.Sprintf("unsupported language: %q", job.Language), ExitCode: 1}
	}
	prepareWorkloadCmd(cmd, shellWorkspaceDir, nil)

	stdout := newLimitedBuffer(maxStdoutBytes)
	stderr := newLimitedBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			if stderr.Len() == 0 {
				_, _ = stderr.WriteString(err.Error())
			}
		}
	}

	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

func needsChartPrelude(code string) bool {
	lower := strings.ToLower(code)
	return strings.Contains(lower, "plotly") || strings.Contains(lower, "matplotlib")
}

// buildPythonCmd 将代码写入临时文件后执行，避免 -c 参数引号转义问题。
func buildPythonCmd(ctx context.Context, code string) *exec.Cmd {
	if needsChartPrelude(code) {
		code = chartPreludeStmt + code
	}

	if err := ensureWorkloadBaseDirs(); err != nil {
		return exec.CommandContext(ctx, python3Bin, "-c", code)
	}

	f, err := os.CreateTemp(shellTempDir, "exec-*.py")
	if err != nil {
		// 降级为 -c 模式
		return exec.CommandContext(ctx, python3Bin, "-c", code)
	}
	_, _ = f.WriteString(code)
	_ = f.Close()
	_ = chownIfPossible(f.Name())

	cmd := exec.CommandContext(ctx, python3Bin, f.Name())
	// 执行后清理临时文件
	go func() {
		<-ctx.Done()
		_ = os.Remove(f.Name())
	}()
	return cmd
}

func writeJSON(conn net.Conn, v any) {
	_ = json.NewEncoder(conn).Encode(v)
}

func fetchArtifacts() FetchArtifactsResult {
	return fetchArtifactsFromDir(artifactOutputDir)
}

func fetchArtifactsFromDir(dir string) FetchArtifactsResult {
	resolvedDir := dir
	if evalDir, err := filepath.EvalSymlinks(dir); err == nil {
		resolvedDir = evalDir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return FetchArtifactsResult{}
	}

	var artifacts []ArtifactEntry
	var totalSize int64
	truncated := false

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 拒绝 symlink，防止通过符号链接读取宿主机文件
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if len(artifacts) >= maxArtifactFiles {
			truncated = true
			break
		}

		// 只取基础文件名，过滤路径穿越
		safeName := filepath.Base(entry.Name())
		if safeName == "." || safeName == ".." || safeName == "" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		fileSize := info.Size()
		if fileSize > maxArtifactBytes {
			truncated = true
			continue
		}
		if totalSize+fileSize > maxTotalArtifacts {
			truncated = true
			break
		}

		fullPath := filepath.Join(dir, safeName)
		// 校验解析后的路径仍在 output 目录内
		resolved, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			continue
		}
		if !isWithinDir(resolvedDir, resolved) {
			continue
		}

		data, err := readFileLimited(resolved, maxArtifactBytes)
		if err != nil {
			continue
		}

		mimeType := detectMimeType(data)
		artifacts = append(artifacts, ArtifactEntry{
			Filename: safeName,
			Size:     int64(len(data)),
			MimeType: mimeType,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
		totalSize += int64(len(data))
	}

	if artifacts == nil {
		artifacts = []ArtifactEntry{}
	}
	return FetchArtifactsResult{Artifacts: artifacts, Truncated: truncated}
}

func isWithinDir(dir string, path string) bool {
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..") && rel != ""
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func detectMimeType(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}
	if isSVGContent(data) {
		return "image/svg+xml"
	}
	mimeType := nethttp.DetectContentType(data)
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil && parsed != "" {
		mimeType = parsed
	}
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}

func isSVGContent(data []byte) bool {
	trimmed := bytes.TrimLeft(data, "\ufeff\t\n\f\r ")
	if len(trimmed) == 0 {
		return false
	}
	decoder := xml.NewDecoder(bytes.NewReader(trimmed))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch element := token.(type) {
		case xml.StartElement:
			return strings.EqualFold(element.Name.Local, "svg")
		}
	}
}
