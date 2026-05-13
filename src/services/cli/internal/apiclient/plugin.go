package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type PluginPackage struct {
	ID          string          `json:"id"`
	PackageID   string          `json:"package_id"`
	Version     string          `json:"version"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description,omitempty"`
	Manifest    json.RawMessage `json:"manifest"`
	SourceKind  string          `json:"source_kind"`
	SourceURI   string          `json:"source_uri,omitempty"`
	IsActive    bool            `json:"is_active"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type PluginInstallRequest struct {
	ManifestPath string `json:"manifest_path,omitempty"`
	SourceKind   string `json:"source_kind,omitempty"`
	SourceURI    string `json:"source_uri,omitempty"`
}

type PluginEnablementRequest struct {
	WorkspaceRef string         `json:"workspace_ref,omitempty"`
	Enabled      bool           `json:"enabled"`
	Settings     map[string]any `json:"settings,omitempty"`
}

type PluginSettingsRequest struct {
	WorkspaceRef string         `json:"workspace_ref,omitempty"`
	Settings     map[string]any `json:"settings"`
}

type PluginEnablement struct {
	ID              string          `json:"id"`
	AccountID       string          `json:"account_id"`
	PackageID       string          `json:"package_id"`
	PluginID        string          `json:"plugin_id"`
	PluginVersion   string          `json:"plugin_version"`
	ProfileRef      string          `json:"profile_ref"`
	WorkspaceRef    string          `json:"workspace_ref"`
	Enabled         bool            `json:"enabled"`
	EnabledByUserID string          `json:"enabled_by_user_id"`
	SettingsJSON    json.RawMessage `json:"settings"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type PluginRuntimeStatus struct {
	AccountID     string          `json:"account_id,omitempty"`
	PackageID     string          `json:"package_id,omitempty"`
	PluginID      string          `json:"plugin_id,omitempty"`
	PluginVersion string          `json:"plugin_version,omitempty"`
	ProfileRef    string          `json:"profile_ref,omitempty"`
	WorkspaceRef  string          `json:"workspace_ref,omitempty"`
	Status        string          `json:"status,omitempty"`
	StatusJSON    json.RawMessage `json:"status_json,omitempty"`
	UpdatedAt     string          `json:"updated_at,omitempty"`
}

type PluginRegistryItem struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Publisher       string   `json:"publisher"`
	Description     string   `json:"description,omitempty"`
	HostRequirement string   `json:"host_requirement"`
	Platforms       []string `json:"platforms,omitempty"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	CreatedAt       string   `json:"created_at,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
}

func (c *Client) ListPlugins(ctx context.Context) ([]PluginPackage, error) {
	var resp struct {
		Items []PluginPackage `json:"items"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/plugins", nil, &resp); err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	return resp.Items, nil
}

func (c *Client) InstallPlugin(ctx context.Context, req PluginInstallRequest) (PluginPackage, error) {
	var resp PluginPackage
	if err := c.doJSON(ctx, http.MethodPost, "/v1/plugins", req, &resp); err != nil {
		return PluginPackage{}, fmt.Errorf("install plugin: %w", err)
	}
	return resp, nil
}

func (c *Client) GetPlugin(ctx context.Context, pluginID string) (PluginPackage, error) {
	var resp PluginPackage
	if err := c.doJSON(ctx, http.MethodGet, "/v1/plugins/"+url.PathEscape(pluginID), nil, &resp); err != nil {
		return PluginPackage{}, fmt.Errorf("get plugin: %w", err)
	}
	return resp, nil
}

func (c *Client) UninstallPlugin(ctx context.Context, pluginID string) error {
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/plugins/"+url.PathEscape(pluginID), nil, nil); err != nil {
		return fmt.Errorf("uninstall plugin: %w", err)
	}
	return nil
}

func (c *Client) SetPluginEnablement(ctx context.Context, pluginID string, req PluginEnablementRequest) (PluginEnablement, error) {
	var resp PluginEnablement
	path := "/v1/plugins/" + url.PathEscape(pluginID) + "/enablements"
	if err := c.doJSON(ctx, http.MethodPut, path, req, &resp); err != nil {
		return PluginEnablement{}, fmt.Errorf("set plugin enablement: %w", err)
	}
	return resp, nil
}

func (c *Client) UpdatePluginSettings(ctx context.Context, pluginID string, req PluginSettingsRequest) (PluginEnablement, error) {
	var resp PluginEnablement
	path := "/v1/plugins/" + url.PathEscape(pluginID) + "/settings"
	if err := c.doJSON(ctx, http.MethodPatch, path, req, &resp); err != nil {
		return PluginEnablement{}, fmt.Errorf("update plugin settings: %w", err)
	}
	return resp, nil
}

func (c *Client) GetPluginRuntimeStatus(ctx context.Context, pluginID, workspaceRef string) (PluginRuntimeStatus, error) {
	values := url.Values{}
	if workspaceRef != "" {
		values.Set("workspace_ref", workspaceRef)
	}
	path := "/v1/plugins/" + url.PathEscape(pluginID) + "/runtime/status"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp PluginRuntimeStatus
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return PluginRuntimeStatus{}, fmt.Errorf("get plugin runtime status: %w", err)
	}
	return resp, nil
}

func (c *Client) InstallPluginRuntime(ctx context.Context, pluginID, workspaceRef string) (map[string]any, error) {
	req := map[string]string{}
	if workspaceRef != "" {
		req["workspace_ref"] = workspaceRef
	}
	var resp map[string]any
	path := "/v1/plugins/" + url.PathEscape(pluginID) + "/runtime/install"
	if err := c.doJSON(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, fmt.Errorf("install plugin runtime: %w", err)
	}
	return resp, nil
}

func SearchPluginRegistry(ctx context.Context, registryURL string, query string, host string, platform string) ([]PluginRegistryItem, error) {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	if host != "" {
		values.Set("host", host)
	}
	if platform != "" {
		values.Set("platform", platform)
	}
	endpoint := stringsTrimRightSlash(registryURL) + "/api/v1/plugins"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("search plugin registry: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search plugin registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search plugin registry: http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("search plugin registry: read response: %w", err)
	}
	var wrapped struct {
		Items []PluginRegistryItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Items != nil {
		return wrapped.Items, nil
	}
	var items []PluginRegistryItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("search plugin registry: decode response: %w", err)
	}
	return items, nil
}

func stringsTrimRightSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}
