//go:build desktop

package worker

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/desktoprun"
	"arkloop/services/worker/internal/queue"
)

// InitDesktopInfra 创建共享的 job queue 和 event bus，注册到全局状态。
// 在 API 和 Worker 启动之前调用，避免 SQLite 锁竞争。
func InitDesktopInfra() error {
	bus := eventbus.NewLocalEventBus()
	desktop.SetEventBus(bus)

	localNotifier := consumer.NewLocalNotifier()
	desktop.SetWorkNotifier(localNotifier)
	cq, err := queue.NewChannelJobQueue(25, localNotifier.Notify)
	if err != nil {
		return err
	}
	desktop.SetJobEnqueuer(cq)
	desktop.MarkReady()

	// 静默安装 RTK，不阻塞启动流程。
	go ensureRTKDesktop()

	return nil
}

// StartDesktop 启动桌面模式 Worker 消费循环。阻塞直到 ctx 取消或出错。
func StartDesktop(ctx context.Context) error {
	return desktoprun.RunDesktop(ctx)
}

// ensureRTKDesktop 确保 ~/.arkloop/bin/rtk 存在。
// 优先从 PATH 中已有的 rtk 复制；否则通过官方安装脚本下载。
// 所有错误均仅记录日志，不影响主流程。
func ensureRTKDesktop() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	destDir := filepath.Join(home, ".arkloop", "bin")
	destBin := filepath.Join(destDir, "rtk")

	if _, err := os.Stat(destBin); err == nil {
		return // 已安装
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		slog.Warn("rtk: failed to create bin dir", "err", err)
		return
	}

	// 尝试从 PATH 复制已有的 rtk。
	if existing, err := exec.LookPath("rtk"); err == nil {
		if data, err := os.ReadFile(existing); err == nil {
			if err := os.WriteFile(destBin, data, 0755); err == nil {
				slog.Info("rtk: copied from PATH", "src", existing)
				return
			}
		}
	}

	// 从官方安装脚本下载。
	slog.Info("rtk: not found, downloading...")
	script, err := fetchRTKInstallScript()
	if err != nil {
		slog.Warn("rtk: download install script failed", "err", err)
		return
	}

	cmd := exec.Command("sh")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+destDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("rtk: install script failed", "err", err, "output", string(out))
		// 脚本可能安装到 ~/.local/bin/rtk，尝试移动。
		localBin := filepath.Join(home, ".local", "bin", "rtk")
		if _, statErr := os.Stat(localBin); statErr == nil {
			_ = os.Rename(localBin, destBin)
			_ = os.Chmod(destBin, 0755)
		}
		return
	}

	// 安装脚本成功后检查 destBin 是否已到位，否则从 ~/.local/bin 移动。
	if _, err := os.Stat(destBin); err != nil {
		localBin := filepath.Join(home, ".local", "bin", "rtk")
		if _, statErr := os.Stat(localBin); statErr == nil {
			_ = os.Rename(localBin, destBin)
			_ = os.Chmod(destBin, 0755)
		}
	}
	if _, err := os.Stat(destBin); err == nil {
		slog.Info("rtk: installed", "path", destBin)
	}
}

func fetchRTKInstallScript() ([]byte, error) {
	resp, err := http.Get("https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
