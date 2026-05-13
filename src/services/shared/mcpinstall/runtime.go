package mcpinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/mcpoauth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DiscoveryQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type EnabledInstall struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	ProfileRef       string
	InstallKey       string
	DisplayName      string
	SourceKind       string
	SourceURI        *string
	SyncMode         string
	Transport        string
	LaunchSpecJSON   json.RawMessage
	HostRequirement  string
	DiscoveryStatus  string
	LastErrorCode    *string
	LastErrorMessage *string
	LastCheckedAt    *time.Time
	AuthSecretID     *uuid.UUID
	EncryptedValue   *string
	KeyVersion       *int
}

type ServerConfig struct {
	ServerID         string
	AccountID        string
	Transport        string
	URL              string
	Headers          map[string]string
	Command          string
	Args             []string
	Cwd              *string
	Env              map[string]string
	InheritParentEnv bool
	CallTimeoutMs    int
	AuthSecretID     string
	OAuth            *mcpoauth.AuthState
}

type AuthPayload struct {
	Headers map[string]string   `json:"headers,omitempty"`
	Env     map[string]string   `json:"env,omitempty"`
	OAuth   *mcpoauth.AuthState `json:"oauth,omitempty"`
}

func LoadEnabledInstalls(ctx context.Context, pool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string) ([]EnabledInstall, error) {
	if pool == nil {
		return nil, fmt.Errorf("mcp install: pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	profileRef = strings.TrimSpace(profileRef)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if accountID == uuid.Nil || profileRef == "" || workspaceRef == "" {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT i.id, i.account_id, i.profile_ref, i.install_key, i.display_name, i.source_kind, i.source_uri,
		       i.sync_mode, i.transport, i.launch_spec_json, i.host_requirement, i.discovery_status,
		       i.last_error_code, i.last_error_message, i.last_checked_at,
		       i.auth_headers_secret_id, s.encrypted_value, s.key_version
		  FROM workspace_mcp_enablements w
		  JOIN profile_mcp_installs i
		    ON i.id = w.install_id
		   AND i.account_id = w.account_id
		 LEFT JOIN secrets s ON s.id = i.auth_headers_secret_id
		 WHERE w.account_id = $1
		   AND w.workspace_ref = $2
		   AND w.enabled = TRUE
		   AND i.profile_ref = $3
		 ORDER BY i.created_at ASC
	`, accountID, workspaceRef, profileRef)
	if err != nil {
		return nil, fmt.Errorf("mcp install: query enabled installs: %w", err)
	}
	defer rows.Close()

	installs := make([]EnabledInstall, 0)
	for rows.Next() {
		var item EnabledInstall
		if err := rows.Scan(
			&item.ID,
			&item.AccountID,
			&item.ProfileRef,
			&item.InstallKey,
			&item.DisplayName,
			&item.SourceKind,
			&item.SourceURI,
			&item.SyncMode,
			&item.Transport,
			&item.LaunchSpecJSON,
			&item.HostRequirement,
			&item.DiscoveryStatus,
			&item.LastErrorCode,
			&item.LastErrorMessage,
			&item.LastCheckedAt,
			&item.AuthSecretID,
			&item.EncryptedValue,
			&item.KeyVersion,
		); err != nil {
			return nil, fmt.Errorf("mcp install: scan enabled install: %w", err)
		}
		installs = append(installs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mcp install: enabled installs rows: %w", err)
	}
	return installs, nil
}

func ParseServerConfig(serverID string, payload map[string]any, defaultTimeoutMs int) (ServerConfig, error) {
	cleanedID := strings.TrimSpace(serverID)
	if cleanedID == "" {
		return ServerConfig{}, fmt.Errorf("mcp server id must not be empty")
	}

	transport := strings.ToLower(strings.TrimSpace(asString(payload["transport"])))
	if transport == "" {
		transport = "stdio"
	}

	timeout := defaultTimeoutMs
	rawTimeout := payload["callTimeoutMs"]
	if rawTimeout == nil {
		rawTimeout = payload["call_timeout_ms"]
	}
	if rawTimeout != nil {
		switch typed := rawTimeout.(type) {
		case float64:
			timeout = int(typed)
			if typed != float64(timeout) {
				return ServerConfig{}, fmt.Errorf("mcp server %q call timeout must be an integer", cleanedID)
			}
		case int:
			timeout = typed
		case int64:
			timeout = int(typed)
		default:
			return ServerConfig{}, fmt.Errorf("mcp server %q call timeout must be an integer", cleanedID)
		}
	}
	if timeout <= 0 {
		return ServerConfig{}, fmt.Errorf("mcp server %q call timeout must be a positive integer", cleanedID)
	}

	server := ServerConfig{
		ServerID:         cleanedID,
		Transport:        transport,
		Headers:          map[string]string{},
		Env:              map[string]string{},
		InheritParentEnv: false,
		CallTimeoutMs:    timeout,
	}

	switch transport {
	case "http_sse", "streamable_http":
		server.URL = strings.TrimSpace(asString(payload["url"]))
		if server.URL == "" {
			return ServerConfig{}, fmt.Errorf("mcp server %q missing url", cleanedID)
		}
		if rawHeaders, ok := payload["headers"].(map[string]any); ok {
			for key, value := range rawHeaders {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				server.Headers[key] = asString(value)
			}
		}
		if token := strings.TrimSpace(asString(payload["bearer_token"])); token != "" {
			server.Headers["Authorization"] = "Bearer " + token
		}
	case "stdio":
		server.Command = strings.TrimSpace(asString(payload["command"]))
		if server.Command == "" {
			return ServerConfig{}, fmt.Errorf("mcp server %q missing command", cleanedID)
		}
		server.Args = toStringSlice(payload["args"])
		server.Cwd = optionalStringPtr(firstNonNil(payload["cwd"], payload["working_dir"], payload["workingDir"]))
		server.InheritParentEnv = asBool(firstNonNil(payload["inherit_parent_env"], payload["inheritParentEnv"]))
		if rawEnv, ok := payload["env"].(map[string]any); ok {
			for key, value := range rawEnv {
				key = strings.TrimSpace(key)
				if key == "" {
					return ServerConfig{}, fmt.Errorf("mcp server %q env key invalid", cleanedID)
				}
				server.Env[key] = asString(value)
			}
		}
	default:
		return ServerConfig{}, fmt.Errorf("mcp server %q transport not supported: %s", cleanedID, transport)
	}

	return server, nil
}

func ServerConfigFromInstall(install EnabledInstall, headers map[string]string, defaultTimeoutMs int) (ServerConfig, error) {
	return ServerConfigFromInstallWithAuth(install, AuthPayload{Headers: headers}, defaultTimeoutMs)
}

func ServerConfigFromInstallWithAuth(install EnabledInstall, auth AuthPayload, defaultTimeoutMs int) (ServerConfig, error) {
	spec := map[string]any{}
	if len(install.LaunchSpecJSON) > 0 {
		if err := json.Unmarshal(install.LaunchSpecJSON, &spec); err != nil {
			return ServerConfig{}, fmt.Errorf("launch spec is invalid")
		}
	}
	if strings.TrimSpace(install.Transport) != "" {
		if rawTransport, ok := spec["transport"]; !ok || strings.TrimSpace(asString(rawTransport)) == "" {
			spec["transport"] = strings.TrimSpace(install.Transport)
		}
	}
	server, err := ParseServerConfig(install.InstallKey, spec, defaultTimeoutMs)
	if err != nil {
		return ServerConfig{}, err
	}
	server.AccountID = install.AccountID.String()
	if install.AuthSecretID != nil {
		server.AuthSecretID = install.AuthSecretID.String()
	}
	server.OAuth = auth.OAuth
	if len(auth.Headers) > 0 {
		if server.Headers == nil {
			server.Headers = map[string]string{}
		}
		for key, value := range auth.Headers {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			server.Headers[key] = value
		}
	}
	if len(auth.Env) > 0 {
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		for key, value := range auth.Env {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			server.Env[key] = value
		}
	}
	return server, nil
}

type InstallSecretDecryptor func(ctx context.Context, encrypted string, keyVersion *int) ([]byte, error)

func ServerConfigsFromInstalls(ctx context.Context, installs []EnabledInstall, decrypt InstallSecretDecryptor, defaultTimeoutMs int) []ServerConfig {
	servers := make([]ServerConfig, 0, len(installs))
	for _, install := range installs {
		auth, ok := decryptInstallAuthPayload(ctx, install, decrypt)
		if !ok {
			continue
		}
		server, err := ServerConfigFromInstallWithAuth(install, auth, defaultTimeoutMs)
		if err != nil {
			continue
		}
		if err := CheckHostRequirement(server, install.HostRequirement); err != nil {
			continue
		}
		servers = append(servers, server)
	}
	return servers
}

func decryptInstallHeaders(ctx context.Context, install EnabledInstall, decrypt InstallSecretDecryptor) (map[string]string, bool) {
	auth, ok := decryptInstallAuthPayload(ctx, install, decrypt)
	return auth.Headers, ok
}

func decryptInstallAuthPayload(ctx context.Context, install EnabledInstall, decrypt InstallSecretDecryptor) (AuthPayload, bool) {
	headers := map[string]string{}
	auth := AuthPayload{Headers: headers}
	if install.EncryptedValue == nil {
		if install.KeyVersion != nil {
			return AuthPayload{}, false
		}
		return auth, true
	}
	if decrypt == nil {
		return AuthPayload{}, false
	}
	payload, err := decrypt(ctx, *install.EncryptedValue, install.KeyVersion)
	if err != nil {
		return AuthPayload{}, false
	}
	if len(payload) == 0 {
		return auth, true
	}
	decoded, err := DecodeAuthPayload(payload)
	if err == nil {
		return decoded, true
	}
	if err := json.Unmarshal(payload, &headers); err != nil {
		token := strings.TrimSpace(string(payload))
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
	}
	auth.Headers = headers
	return auth, true
}

func DecodeAuthPayload(payload []byte) (AuthPayload, error) {
	var structured AuthPayload
	if err := json.Unmarshal(payload, &structured); err == nil && (len(structured.Headers) > 0 || len(structured.Env) > 0 || structured.OAuth != nil) {
		structured.Headers = cleanStringMap(structured.Headers)
		structured.Env = cleanStringMap(structured.Env)
		return structured, nil
	}

	headers := map[string]string{}
	if err := json.Unmarshal(payload, &headers); err != nil {
		return AuthPayload{}, err
	}
	return AuthPayload{Headers: cleanStringMap(headers)}, nil
}

func CheckHostRequirement(server ServerConfig, requirement string) error {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return nil
	}
	switch requirement {
	case "remote_http":
		if server.Transport == "stdio" {
			return fmt.Errorf("remote_http host does not support stdio launch specs")
		}
	case "cloud_worker":
		if server.Transport == "stdio" && server.Command == "" {
			return fmt.Errorf("stdio command missing")
		}
	case "desktop_local", "desktop_sidecar":
		if !desktopHostRequirementsAvailable {
			return fmt.Errorf("%s host is only available in desktop mode", requirement)
		}
		return nil
	}
	return nil
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(asString(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func optionalStringPtr(value any) *string {
	text := strings.TrimSpace(asString(value))
	if text == "" {
		return nil
	}
	return &text
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func asBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func cleanStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = item
	}
	return out
}
