package pluginbinary

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type DownloadConfig struct {
	URL        string
	SHA256     string
	TargetDir  string
	TargetPath string
}

type ArchiveStore interface {
	Write(ctx context.Context, pluginID, version, relPath string, data []byte) error
}

type archiveStoreWithMode interface {
	WriteWithMode(ctx context.Context, pluginID, version, relPath string, data []byte, mode os.FileMode) error
}

func DownloadAndExtract(ctx context.Context, client *http.Client, store ArchiveStore, pluginID, version string, cfg DownloadConfig) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(cfg.URL), nil)
	if err != nil {
		return fmt.Errorf("create plugin binary request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download plugin binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download plugin binary status: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read plugin binary archive: %w", err)
	}
	if err := verifySHA256(data, cfg.SHA256); err != nil {
		return err
	}
	return extractTarGzip(ctx, store, pluginID, version, data, cfg.TargetDir, cfg.TargetPath)
}

func ExtractTarGzip(ctx context.Context, store ArchiveStore, pluginID, version string, data []byte, targetDir ...string) error {
	return extractTarGzip(ctx, store, pluginID, version, data, firstTargetDir(targetDir), "")
}

func extractTarGzip(ctx context.Context, store ArchiveStore, pluginID, version string, data []byte, targetDir, targetPath string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open plugin binary gzip: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)
	written := map[string]struct{}{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read plugin binary tar: %w", err)
		}
		name := strings.TrimSpace(header.Name)
		if name == "" {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			content, err := io.ReadAll(tarReader)
			if err != nil {
				return fmt.Errorf("read plugin binary file %q: %w", name, err)
			}
			cleanName, err := archiveEntryPath(name)
			if err != nil {
				return fmt.Errorf("plugin binary archive entry %q: %w", name, err)
			}
			relPath := prefixedArchivePath(targetDir, archiveTargetPath(cleanName, targetPath))
			if _, exists := written[relPath]; exists {
				return fmt.Errorf("plugin binary archive maps multiple files to %q", relPath)
			}
			written[relPath] = struct{}{}
			if modeStore, ok := store.(archiveStoreWithMode); ok {
				mode := header.FileInfo().Mode().Perm()
				if mode == 0 {
					mode = 0o600
				}
				if err := modeStore.WriteWithMode(ctx, pluginID, version, relPath, content, mode); err != nil {
					return fmt.Errorf("extract plugin binary file %q: %w", name, err)
				}
				continue
			}
			if err := store.Write(ctx, pluginID, version, relPath, content); err != nil {
				return fmt.Errorf("extract plugin binary file %q: %w", name, err)
			}
		default:
			return fmt.Errorf("plugin binary archive contains unsupported entry %q", name)
		}
	}
}

func archiveTargetPath(cleanName, targetPath string) string {
	targetPath = strings.Trim(strings.TrimSpace(targetPath), "/")
	if targetPath != "" && path.Base(cleanName) == path.Base(targetPath) {
		return targetPath
	}
	return cleanName
}

func firstTargetDir(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(values[0]), "/")
}

func archiveEntryPath(name string) (string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("path must be relative")
	}
	name = strings.Trim(raw, "/")
	if name == "" || name == "." {
		return "", fmt.Errorf("path is invalid")
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path escapes archive root")
	}
	return cleaned, nil
}

func prefixedArchivePath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return filepath.ToSlash(filepath.Join(prefix, name))
}

func verifySHA256(data []byte, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return fmt.Errorf("plugin binary sha256 must not be empty")
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("plugin binary sha256 mismatch")
	}
	return nil
}
