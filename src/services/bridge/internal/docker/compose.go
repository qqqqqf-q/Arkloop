package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Logger is the interface used by Compose for structured logging.
type Logger interface {
	Info(msg string, extra map[string]any)
	Error(msg string, extra map[string]any)
}

// Compose wraps Docker Compose CLI operations for a single project directory.
type Compose struct {
	projectDir  string
	composeFile string
	logger      Logger
	moduleLocks sync.Map // map[serviceName]bool — true means busy
}

// NewCompose creates a Compose wrapper for the given project directory.
// The directory must contain a compose.yaml file.
func NewCompose(projectDir string, logger Logger) *Compose {
	return &Compose{
		projectDir:  projectDir,
		composeFile: filepath.Join(projectDir, "compose.yaml"),
		logger:      logger,
	}
}

// psEntry represents a single row from `docker compose ps --format json`.
type psEntry struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// ContainerStatus returns the module status for a Docker Compose service.
// Possible return values: "running", "error", "stopped", "not_installed".
func (c *Compose) ContainerStatus(ctx context.Context, serviceName string, profile string) (string, error) {
	if err := c.validateProjectDir(); err != nil {
		return "", err
	}

	args := c.baseArgs()
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "ps", "--all", "--format", "json", serviceName)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = c.projectDir

	out, err := cmd.Output()
	if err != nil {
		// If docker compose ps fails or returns nothing, the service is not installed.
		return "not_installed", nil
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return "not_installed", nil
	}

	// docker compose ps --format json may emit one JSON object per line.
	var entries []psEntry
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry psEntry
		if jsonErr := json.Unmarshal([]byte(line), &entry); jsonErr != nil {
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return "not_installed", nil
	}

	return mapStatus(entries[0]), nil
}

// Install pulls/builds and starts a service. If profile is non-empty it is
// passed as --profile to docker compose.
func (c *Compose) Install(ctx context.Context, serviceName string, profile string) (*Operation, error) {
	if err := c.validateProjectDir(); err != nil {
		return nil, err
	}

	args := c.baseArgs()
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "up", "-d", "--build", serviceName)

	return c.runAsync(ctx, serviceName, "install", args)
}

// Start starts an existing service container.
func (c *Compose) Start(ctx context.Context, serviceName string) (*Operation, error) {
	if err := c.validateProjectDir(); err != nil {
		return nil, err
	}

	args := c.baseArgs()
	args = append(args, "start", serviceName)

	return c.runAsync(ctx, serviceName, "start", args)
}

// Stop stops a running service container.
func (c *Compose) Stop(ctx context.Context, serviceName string) (*Operation, error) {
	if err := c.validateProjectDir(); err != nil {
		return nil, err
	}

	args := c.baseArgs()
	args = append(args, "stop", serviceName)

	return c.runAsync(ctx, serviceName, "stop", args)
}

// Restart restarts a service container.
func (c *Compose) Restart(ctx context.Context, serviceName string) (*Operation, error) {
	if err := c.validateProjectDir(); err != nil {
		return nil, err
	}

	args := c.baseArgs()
	args = append(args, "restart", serviceName)

	return c.runAsync(ctx, serviceName, "restart", args)
}

// ProjectDir returns the compose project root directory.
func (c *Compose) ProjectDir() string {
	return c.projectDir
}

// baseArgs returns the shared docker compose CLI arguments.
func (c *Compose) baseArgs() []string {
	return []string{"compose", "-f", c.composeFile}
}

// validateProjectDir checks that the project directory contains compose.yaml.
func (c *Compose) validateProjectDir() error {
	info, err := os.Stat(c.composeFile)
	if err != nil {
		return fmt.Errorf("compose file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("compose file path is a directory: %s", c.composeFile)
	}
	return nil
}

// runAsync starts a docker compose command in the background, streaming its
// combined output into an Operation.
func (c *Compose) runAsync(ctx context.Context, serviceName, action string, args []string) (*Operation, error) {
	// Check if module already has an active operation
	if _, loaded := c.moduleLocks.LoadOrStore(serviceName, true); loaded {
		return nil, fmt.Errorf("module %q already has an active operation", serviceName)
	}

	op := NewOperation(serviceName, action)
	op.Status = OperationRunning

	// Create a cancellable context for this operation
	cancelCtx, cancel := context.WithCancel(ctx)
	op.cancelFunc = cancel

	cmd := exec.CommandContext(cancelCtx, "docker", args...)
	cmd.Dir = c.projectDir

	// Merge stdout and stderr so we capture all output.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		c.moduleLocks.Delete(serviceName)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // redirect stderr into the same pipe
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	c.logger.Info("running docker compose", map[string]any{
		"operation_id": op.ID,
		"action":       action,
		"service":      serviceName,
		"args":         args,
	})

	if err := cmd.Start(); err != nil {
		cancel()
		c.moduleLocks.Delete(serviceName)
		return nil, fmt.Errorf("start command: %w", err)
	}

	if cmd.Process != nil {
		op.SetPID(cmd.Process.Pid)
	}

	go func() {
		c.streamOutput(op, cmd, stdoutPipe)
		c.moduleLocks.Delete(serviceName)
	}()

	return op, nil
}

// streamOutput reads the combined output line-by-line, appends each to the
// operation log, and completes the operation when the process exits.
func (c *Compose) streamOutput(op *Operation, cmd *exec.Cmd, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		op.AppendLog(scanner.Text())
	}

	err := cmd.Wait()
	if err != nil {
		if op.IsCancelled() {
			op.AppendLog("--- Operation cancelled by user ---")
		}
		c.logger.Error("docker compose failed", map[string]any{
			"operation_id": op.ID,
			"action":       op.Action,
			"service":      op.ModuleID,
			"error":        err.Error(),
		})
	} else {
		c.logger.Info("docker compose completed", map[string]any{
			"operation_id": op.ID,
			"action":       op.Action,
			"service":      op.ModuleID,
		})
	}

	op.Complete(err)
}

// mapStatus converts Docker container state/health into a module status string.
func mapStatus(e psEntry) string {
	switch strings.ToLower(e.State) {
	case "running":
		switch strings.ToLower(e.Health) {
		case "unhealthy":
			return "error"
		default:
			// "healthy", "", or no healthcheck → running
			return "running"
		}
	case "exited", "created":
		return "stopped"
	default:
		return "not_installed"
	}
}
