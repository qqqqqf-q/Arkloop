package pluginregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Plugin struct {
	ID            string `json:"id"`
	LatestVersion string `json:"latest_version"`
}

type Version struct {
	PluginID        string          `json:"plugin_id"`
	Version         string          `json:"version"`
	Manifest        json.RawMessage `json:"manifest"`
	BundleSHA256    string          `json:"bundle_sha256"`
	BundleSizeBytes int64           `json:"bundle_size_bytes"`
}

type pluginResponse struct {
	Plugin Plugin `json:"plugin"`
}

func (c Client) GetLatestVersion(ctx context.Context, pluginID string) (Version, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return Version{}, fmt.Errorf("plugin id is required")
	}
	var detail pluginResponse
	if err := c.getJSON(ctx, "/api/v1/plugins/"+url.PathEscape(pluginID), &detail); err != nil {
		return Version{}, err
	}
	version := strings.TrimSpace(detail.Plugin.LatestVersion)
	if version == "" {
		return Version{}, fmt.Errorf("plugin registry version is missing")
	}
	return c.GetVersion(ctx, pluginID, version)
}

func (c Client) GetVersion(ctx context.Context, pluginID, version string) (Version, error) {
	var out Version
	if err := c.getJSON(ctx, "/api/v1/plugins/"+url.PathEscape(pluginID)+"/versions/"+url.PathEscape(version), &out); err != nil {
		return Version{}, err
	}
	return out, nil
}

func (c Client) GetBundle(ctx context.Context, pluginID, version string) ([]byte, error) {
	return c.getBytes(ctx, "/api/v1/plugins/"+url.PathEscape(pluginID)+"/versions/"+url.PathEscape(version)+"/bundle")
}

func (c Client) GetManifest(ctx context.Context, pluginID, version string) ([]byte, error) {
	return c.getBytes(ctx, "/api/v1/plugins/"+url.PathEscape(pluginID)+"/versions/"+url.PathEscape(version)+"/manifest")
}

func (c Client) getJSON(ctx context.Context, path string, dest any) error {
	data, err := c.getBytes(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode plugin registry response: %w", err)
	}
	return nil
}

func (c Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("plugin registry URL is not configured")
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build plugin registry request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plugin registry request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin registry resource not found")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plugin registry http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read plugin registry response: %w", err)
	}
	return data, nil
}
