package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"
)

// ErrFileExceedsMaxBytes is returned when a Telegram file exceeds the configured size limit.
var ErrFileExceedsMaxBytes = errors.New("file exceeds maxBytes")

const defaultFileDownloadTimeout = 30 * time.Second

// TelegramFile is the getFile result.
type TelegramFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	FilePath     string `json:"file_path"`
}

// GetFile resolves file_id to a path on Telegram CDN (via Bot API).
func (c *Client) GetFile(ctx context.Context, token, fileID string) (*TelegramFile, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("telegrambot: file_id is required")
	}
	var f TelegramFile
	if err := c.callJSON(ctx, token, "getFile", map[string]string{"file_id": fileID}, &f); err != nil {
		return nil, err
	}
	if strings.TrimSpace(f.FilePath) == "" {
		return nil, fmt.Errorf("telegrambot: getFile returned empty file_path")
	}
	return &f, nil
}

// DownloadBotFile fetches bytes from https://api.telegram.org/file/bot<token>/<file_path> (or custom baseURL).
// maxBytes is a hard read cap (e.g. max image size + 1 to detect overflow).
func (c *Client) DownloadBotFile(ctx context.Context, token, filePath string, maxBytes int64) ([]byte, string, error) {
	token = strings.TrimSpace(token)
	filePath = strings.TrimSpace(filePath)
	if token == "" {
		return nil, "", fmt.Errorf("telegrambot: token is required")
	}
	if filePath == "" {
		return nil, "", fmt.Errorf("telegrambot: file_path is required")
	}
	if maxBytes <= 0 {
		return nil, "", fmt.Errorf("telegrambot: maxBytes must be positive")
	}
	u := fmt.Sprintf("%s/file/bot%s/%s",
		c.baseURL,
		url.PathEscape(token),
		strings.TrimPrefix(strings.ReplaceAll(filePath, "\\", "/"), "/"))

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("telegrambot: file download request: %w", err)
	}

	cl := &nethttp.Client{Timeout: defaultFileDownloadTimeout}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("telegrambot: file download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("telegrambot: file download status %d", resp.StatusCode)
	}
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("telegrambot: read file body: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("telegrambot: %w", ErrFileExceedsMaxBytes)
	}
	if ct == "" || ct == "application/octet-stream" {
		ct = nethttp.DetectContentType(data)
	}
	return data, ct, nil
}
