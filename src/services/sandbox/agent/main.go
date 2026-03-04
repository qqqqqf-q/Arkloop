// Guest Agent — 编译后置于 microVM rootfs 内 /usr/local/bin/sandbox-agent
//
// 构建命令（在宿主机上交叉编译为静态二进制）：
//   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sandbox-agent ./agent
//
// rootfs 启动时通过 init 系统（OpenRC/busybox init/systemd）自动运行本程序。
// 也可配置为 kernel 的 init 进程：
//   kernel_args: "... init=/usr/local/bin/sandbox-agent"
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
)

const (
	listenPort   = 8080
	maxCodeBytes = 4 * 1024 * 1024 // 4 MB

	artifactOutputDir  = "/tmp/output"
	maxArtifactFiles   = 5
	maxArtifactBytes   = 5 * 1024 * 1024  // 单文件 5 MB
	maxTotalArtifacts  = 10 * 1024 * 1024  // 总上限 10 MB
)

type ExecJob struct {
	Language  string `json:"language"`   // "python" | "shell"
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
	Action  string   `json:"action"`            // "exec" | "fetch_artifacts"
	ExecJob *ExecJob `json:"exec_job,omitempty"`
}

// AgentResponse 是 v2 协议的统一响应。
type AgentResponse struct {
	Action    string                `json:"action"`
	Exec      *ExecResult           `json:"exec,omitempty"`
	Artifacts *FetchArtifactsResult `json:"artifacts,omitempty"`
	Error     string                `json:"error,omitempty"`
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

	default:
		writeJSON(conn, AgentResponse{Action: req.Action, Error: fmt.Sprintf("unknown action: %s", req.Action)})
	}
}

func ensureOutputDir() {
	_ = os.MkdirAll(artifactOutputDir, 0o755)
}

func executeJob(job ExecJob) ExecResult {
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			if stderr.Len() == 0 {
				stderr.WriteString(err.Error())
			}
		}
	}

	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

const python3Bin = "/usr/local/bin/python3"
const chartPreludePath = "/usr/local/share/arkloop/chart_prelude.py"
const chartPreludeStmt = "try:\n exec(open('" + chartPreludePath + "').read())\nexcept FileNotFoundError:\n pass\n"

func needsChartPrelude(code string) bool {
	lower := strings.ToLower(code)
	return strings.Contains(lower, "plotly") || strings.Contains(lower, "matplotlib")
}

// buildPythonCmd 将代码写入临时文件后执行，避免 -c 参数引号转义问题。
func buildPythonCmd(ctx context.Context, code string) *exec.Cmd {
	if needsChartPrelude(code) {
		code = chartPreludeStmt + code
	}

	f, err := os.CreateTemp("", "exec-*.py")
	if err != nil {
		// 降级为 -c 模式
		return exec.CommandContext(ctx, python3Bin, "-c", code)
	}
	_, _ = f.WriteString(code)
	_ = f.Close()

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
	entries, err := os.ReadDir(artifactOutputDir)
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

		fullPath := filepath.Join(artifactOutputDir, safeName)
		// 校验解析后的路径仍在 output 目录内
		resolved, err := filepath.EvalSymlinks(fullPath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(resolved, artifactOutputDir) {
			continue
		}

		data, err := readFileLimited(resolved, maxArtifactBytes)
		if err != nil {
			continue
		}

		mimeType := detectMimeType(safeName)
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

func readFileLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func detectMimeType(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "application/octet-stream"
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}
