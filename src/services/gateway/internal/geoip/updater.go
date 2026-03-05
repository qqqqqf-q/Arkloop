package geoip

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	downloadURL    = "https://download.maxmind.com/app/geoip_download"
	editionID      = "GeoLite2-City"
	mmdbSuffix     = ".mmdb"
	updateInterval = 24 * time.Hour
)

// Updater 负责自动下载和定期更新 GeoLite2 数据库，并提供 hot-reload 的 Lookup 实现。
type Updater struct {
	licenseKey string
	dbPath     string
	logger     Logger

	mu     sync.RWMutex
	reader *MaxMind
}

// Logger 是 Updater 需要的最小日志接口。
type Logger interface {
	Info(msg string, extra map[string]any)
	Error(msg string, extra map[string]any)
}

// NewUpdater 创建 Updater。
// dbPath 是 .mmdb 文件的存放路径（目录不存在会自动创建）。
// licenseKey 是 MaxMind 的 License Key。
func NewUpdater(dbPath, licenseKey string, logger Logger) *Updater {
	return &Updater{
		licenseKey: licenseKey,
		dbPath:     dbPath,
		logger:     logger,
	}
}

// Init 在启动时检查本地文件，不存在则立即下载。成功后打开 reader。
func (u *Updater) Init() error {
	if _, err := os.Stat(u.dbPath); os.IsNotExist(err) {
		u.logger.Info("geoip db not found, downloading", map[string]any{"path": u.dbPath})
		if err := u.download(); err != nil {
			return fmt.Errorf("geoip download: %w", err)
		}
	}
	return u.reload()
}

// Run 启动每日更新循环，阻塞直到 ctx 取消。
func (u *Updater) Run(ctx context.Context) {
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := u.download(); err != nil {
				u.logger.Error("geoip update failed", map[string]any{"error": err.Error()})
				continue
			}
			if err := u.reload(); err != nil {
				u.logger.Error("geoip reload failed", map[string]any{"error": err.Error()})
			} else {
				u.logger.Info("geoip updated", map[string]any{"path": u.dbPath})
			}
		}
	}
}

// LookupIP 实现 Lookup 接口，转发给当前 reader。
func (u *Updater) LookupIP(ip string) Result {
	u.mu.RLock()
	r := u.reader
	u.mu.RUnlock()
	if r == nil {
		return Result{Type: IPTypeUnknown}
	}
	return r.LookupIP(ip)
}

// Close 关闭底层 reader。
func (u *Updater) Close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.reader != nil {
		u.reader.Close()
		u.reader = nil
	}
}

func (u *Updater) reload() error {
	mm, err := NewMaxMind(u.dbPath)
	if err != nil {
		return err
	}

	u.mu.Lock()
	old := u.reader
	u.reader = mm
	u.mu.Unlock()

	if old != nil {
		old.Close()
	}
	return nil
}

func (u *Updater) download() error {
	dir := filepath.Dir(u.dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	url := fmt.Sprintf("%s?edition_id=%s&license_key=%s&suffix=tar.gz", downloadURL, editionID, u.licenseKey)

	resp, err := http.Get(url)
	if err != nil {
		msg := err.Error()
		if u.licenseKey != "" {
			msg = strings.ReplaceAll(msg, u.licenseKey, "[redacted]")
		}
		return fmt.Errorf("http get: %s", msg)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	// 解压 tar.gz，找到 .mmdb 文件
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("mmdb file not found in archive")
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if !strings.HasSuffix(header.Name, mmdbSuffix) {
			continue
		}

		// 写入临时文件后原子 rename，避免读取到半写文件
		tmpPath := u.dbPath + ".tmp"
		f, err := os.Create(tmpPath)
		if err != nil {
			return fmt.Errorf("create tmp: %w", err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("copy: %w", err)
		}
		f.Close()

		if err := os.Rename(tmpPath, u.dbPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename: %w", err)
		}
		return nil
	}
}
