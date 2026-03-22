//go:build desktop

package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

const executionModeFileName = "execution_mode"

func executionModeFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arkloop", executionModeFileName), nil
}

// RestoreExecutionModeFromDisk 从 ~/.arkloop/execution_mode 恢复用户上次选择的执行模式。
func RestoreExecutionModeFromDisk() {
	p, err := executionModeFilePath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	mode := strings.TrimSpace(string(data))
	if mode == "local" || mode == "vm" {
		SetExecutionMode(mode)
	}
}

// PersistExecutionMode 将执行模式写入磁盘，供下次侧car 启动恢复。
func PersistExecutionMode(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "local" && mode != "vm" {
		return nil
	}
	p, err := executionModeFilePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(mode), 0o600)
}
