//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const activityRecordServiceLabel = "com.arkloop.activity-record"

func installActivityRecordService(config daemonConfig, replaceExecutable bool) (serviceStatus, error) {
	sourceExecutable, err := os.Executable()
	if err != nil {
		return serviceStatus{}, fmt.Errorf("resolve executable: %w", err)
	}
	sourceExecutable, err = filepath.Abs(sourceExecutable)
	if err != nil {
		return serviceStatus{}, fmt.Errorf("resolve executable path: %w", err)
	}
	paths, err := activityRecordServicePaths(config.DataDir)
	if err != nil {
		return serviceStatus{}, err
	}
	executable, err := installServiceExecutable(sourceExecutable, paths.executable, replaceExecutable)
	if err != nil {
		return serviceStatus{}, err
	}
	executablePayload, err := os.ReadFile(executable)
	if err != nil {
		return serviceStatus{}, fmt.Errorf("read executable: %w", err)
	}
	config.ExecutableSHA256 = fmt.Sprintf("%x", sha256.Sum256(executablePayload))
	if err := os.MkdirAll(filepath.Dir(paths.config), 0o700); err != nil {
		return serviceStatus{}, fmt.Errorf("create service data directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.log), 0o755); err != nil {
		return serviceStatus{}, fmt.Errorf("create service log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.plist), 0o755); err != nil {
		return serviceStatus{}, fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	configPayload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return serviceStatus{}, fmt.Errorf("encode daemon config: %w", err)
	}
	configPayload = append(configPayload, '\n')
	plistPayload, err := launchAgentPlist(executable, paths.config, paths.log)
	if err != nil {
		return serviceStatus{}, err
	}

	configUnchanged := fileContentEquals(paths.config, configPayload)
	plistUnchanged := fileContentEquals(paths.plist, plistPayload)
	loaded := launchAgentLoaded()
	pid := readServicePID(paths.pid)
	if configUnchanged && plistUnchanged && loaded && processAlive(pid) {
		_ = os.Chmod(paths.config, 0o600)
		return serviceStatusForPaths(paths), nil
	}

	if loaded {
		_ = launchctl("bootout", launchAgentTarget())
	}
	if err := stopExistingDaemon(paths.pid, 15*time.Second); err != nil {
		return serviceStatus{}, err
	}
	if err := writeFileAtomic(paths.config, configPayload, 0o600); err != nil {
		return serviceStatus{}, fmt.Errorf("write daemon config: %w", err)
	}
	if err := writeFileAtomic(paths.plist, plistPayload, 0o644); err != nil {
		return serviceStatus{}, fmt.Errorf("write LaunchAgent plist: %w", err)
	}
	if err := launchctl("bootstrap", launchAgentDomain(), paths.plist); err != nil {
		return serviceStatus{}, err
	}
	if err := waitForServiceReady(paths, 45*time.Second); err != nil {
		_ = launchctl("kickstart", "-k", launchAgentTarget())
		if retryErr := waitForServiceReady(paths, 45*time.Second); retryErr != nil {
			return serviceStatus{}, errors.Join(err, retryErr)
		}
	}
	return serviceStatusForPaths(paths), nil
}

func activityRecordServiceStatus(dataDir string) (serviceStatus, error) {
	paths, err := activityRecordServicePaths(dataDir)
	if err != nil {
		return serviceStatus{}, err
	}
	return serviceStatusForPaths(paths), nil
}

func uninstallActivityRecordService(dataDir string) (serviceStatus, error) {
	paths, err := activityRecordServicePaths(dataDir)
	if err != nil {
		return serviceStatus{}, err
	}
	if launchAgentLoaded() {
		if err := launchctl("bootout", launchAgentTarget()); err != nil {
			return serviceStatus{}, err
		}
	}
	if err := stopExistingDaemon(paths.pid, 15*time.Second); err != nil {
		return serviceStatus{}, err
	}
	for _, path := range []string{paths.plist, paths.config, paths.pid, paths.status} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return serviceStatus{}, fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return serviceStatusForPaths(paths), nil
}

type servicePaths struct {
	config     string
	plist      string
	pid        string
	status     string
	log        string
	executable string
}

func activityRecordServicePaths(dataDir string) (servicePaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return servicePaths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	return servicePaths{
		config:     filepath.Join(dataDir, "daemon-config.json"),
		plist:      filepath.Join(home, "Library", "LaunchAgents", activityRecordServiceLabel+".plist"),
		pid:        filepath.Join(dataDir, "activity-record.pid"),
		status:     filepath.Join(dataDir, "daemon-status.json"),
		log:        filepath.Join(dataDir, "logs", "daemon.log"),
		executable: filepath.Join(home, ".Arkloop", "bin", "activity-record"),
	}, nil
}

func launchAgentPlist(executable, configPath, logPath string) ([]byte, error) {
	var payload bytes.Buffer
	payload.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	payload.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	payload.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistKeyString(&payload, "Label", activityRecordServiceLabel)
	writePlistKeyString(&payload, "AssociatedBundleIdentifiers", "com.arkloop.desktop")
	payload.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, value := range []string{executable, "daemon", "--config", configPath} {
		payload.WriteString("    <string>")
		if err := xml.EscapeText(&payload, []byte(value)); err != nil {
			return nil, fmt.Errorf("escape LaunchAgent argument: %w", err)
		}
		payload.WriteString("</string>\n")
	}
	payload.WriteString("  </array>\n")
	payload.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	payload.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	writePlistKeyString(&payload, "ProcessType", "Background")
	writePlistKeyString(&payload, "LimitLoadToSessionType", "Aqua")
	payload.WriteString("  <key>ThrottleInterval</key>\n  <integer>10</integer>\n")
	writePlistKeyString(&payload, "StandardOutPath", logPath)
	writePlistKeyString(&payload, "StandardErrorPath", logPath)
	payload.WriteString("</dict>\n</plist>\n")
	return payload.Bytes(), nil
}

func writePlistKeyString(payload *bytes.Buffer, key, value string) {
	payload.WriteString("  <key>")
	_ = xml.EscapeText(payload, []byte(key))
	payload.WriteString("</key>\n  <string>")
	_ = xml.EscapeText(payload, []byte(value))
	payload.WriteString("</string>\n")
}

func serviceStatusForPaths(paths servicePaths) serviceStatus {
	pid := readServicePID(paths.pid)
	_, plistErr := os.Stat(paths.plist)
	status := serviceStatus{
		Installed:      plistErr == nil,
		Loaded:         launchAgentLoaded(),
		Running:        processAlive(pid),
		PID:            pid,
		ConfigPath:     paths.config,
		PlistPath:      paths.plist,
		LogPath:        paths.log,
		ExecutablePath: paths.executable,
	}
	if daemonStatus, ok := readDaemonServiceStatus(paths.status, pid); ok {
		status.AXPermission = &daemonStatus.AXPermission
		status.MicPermission = &daemonStatus.MicPermission
	}
	return status
}

func installServiceExecutable(source, destination string, replace bool) (string, error) {
	if source == destination {
		return destination, nil
	}
	sourcePayload, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read service executable: %w", err)
	}
	destinationPayload, destinationErr := os.ReadFile(destination)
	if destinationErr == nil && bytes.Equal(sourcePayload, destinationPayload) {
		return destination, nil
	}
	if destinationErr == nil && !replace && !strings.Contains(source, ".app/Contents/") {
		return destination, nil
	}
	if destinationErr != nil && !errors.Is(destinationErr, os.ErrNotExist) {
		return "", fmt.Errorf("read installed service executable: %w", destinationErr)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("create service bin directory: %w", err)
	}
	if err := writeFileAtomic(destination, sourcePayload, 0o755); err != nil {
		return "", fmt.Errorf("install service executable: %w", err)
	}
	return destination, nil
}

func launchAgentDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func launchAgentTarget() string {
	return launchAgentDomain() + "/" + activityRecordServiceLabel
}

func launchAgentLoaded() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "launchctl", "print", launchAgentTarget()).Run() == nil
}

func launchctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), message)
	}
	return nil
}

func readServicePID(path string) int {
	payload, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || pid <= 1 {
		return 0
	}
	return pid
}

func stopExistingDaemon(pidPath string, timeout time.Duration) error {
	pid := readServicePID(pidPath)
	if pid <= 1 || !processAlive(pid) {
		_ = os.Remove(pidPath)
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find existing daemon %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop existing daemon %d: %w", pid, err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(pidPath)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("existing daemon %d did not stop within %s", pid, timeout)
}

type daemonServiceStatus struct {
	PID           int  `json:"pid"`
	AXPermission  bool `json:"ax_permission"`
	MicPermission bool `json:"mic_permission"`
}

func readDaemonServiceStatus(path string, pid int) (daemonServiceStatus, bool) {
	var status daemonServiceStatus
	payload, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(payload, &status) != nil || status.PID != pid {
		return daemonServiceStatus{}, false
	}
	return status, true
}

func waitForServiceReady(paths servicePaths, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid := readServicePID(paths.pid); processAlive(pid) {
			if _, ok := readDaemonServiceStatus(paths.status, pid); ok {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("activity-record service did not start within %s", timeout)
}

func fileContentEquals(path string, expected []byte) bool {
	payload, err := os.ReadFile(path)
	return err == nil && bytes.Equal(payload, expected)
}

func writeFileAtomic(path string, payload []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".activity-record-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
