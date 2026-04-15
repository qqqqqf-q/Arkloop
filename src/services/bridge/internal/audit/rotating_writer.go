package audit

import (
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultMaxAuditLogBytes = 10 * 1024 * 1024
	defaultMaxAuditBackups  = 3
)

type RotatingFileWriter struct {
	path       string
	maxBytes   int64
	maxBackups int
	mu         sync.Mutex
}

func NewRotatingFileWriter(path string, maxBytes int64, maxBackups int) *RotatingFileWriter {
	if maxBytes <= 0 {
		maxBytes = defaultMaxAuditLogBytes
	}
	if maxBackups <= 0 {
		maxBackups = defaultMaxAuditBackups
	}
	return &RotatingFileWriter{
		path:       path,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
	}
}

func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return 0, err
	}
	if err := w.rotateIfNeeded(int64(len(p))); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	return f.Write(p)
}

func (w *RotatingFileWriter) rotateIfNeeded(incoming int64) error {
	info, err := os.Stat(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size()+incoming <= w.maxBytes {
		return nil
	}
	for i := w.maxBackups; i >= 1; i-- {
		src := w.path + "." + itoa(i)
		dst := w.path + "." + itoa(i+1)
		if i == w.maxBackups {
			_ = os.Remove(src)
			continue
		}
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for v > 0 {
		pos--
		buf[pos] = byte('0' + (v % 10))
		v /= 10
	}
	return string(buf[pos:])
}
