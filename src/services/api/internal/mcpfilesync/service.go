//go:build desktop

package mcpfilesync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	shareddesktop "arkloop/services/shared/desktop"

	"github.com/google/uuid"
)

type DiscoveryRequest struct {
	WorkspaceRoot *string
	Paths         []string
}

type ProposedInstall struct {
	InstallKey      string            `json:"install_key"`
	DisplayName     string            `json:"display_name"`
	Transport       string            `json:"transport"`
	LaunchSpecJSON  json.RawMessage   `json:"launch_spec_json"`
	HostRequirement string            `json:"host_requirement"`
	SourceKind      string            `json:"source_kind"`
	SourceURI       string            `json:"source_uri"`
	SyncMode        string            `json:"sync_mode"`
	HasAuth         bool              `json:"has_auth"`
	AuthHeaders     map[string]string `json:"-"`
}

type ImportRequest struct {
	SourceURI  string
	InstallKey string
}

type ImportedInstall struct {
	Install     data.ProfileMCPInstall
	AuthHeaders map[string]string
}

type DiscoverySource struct {
	SourceURI        string            `json:"source_uri"`
	SourceKind       string            `json:"source_kind"`
	Installable      bool              `json:"installable"`
	ValidationErrors []string          `json:"validation_errors"`
	HostWarnings     []string          `json:"host_warnings"`
	ProposedInstalls []ProposedInstall `json:"proposed_installs"`
}

type DiscoveryResponse struct {
	Sources []DiscoverySource `json:"sources"`
}

type Service struct {
	installsRepo *data.ProfileMCPInstallsRepository
	secretsRepo  *data.SecretsRepository
	pool         data.DB
	dataDir      string

	mu           sync.Mutex
	lastModified time.Time
}

func NewService(dataDir string, installsRepo *data.ProfileMCPInstallsRepository, secretsRepo *data.SecretsRepository, pool data.DB) (*Service, error) {
	if installsRepo == nil {
		return nil, fmt.Errorf("installsRepo must not be nil")
	}
	return &Service{
		installsRepo: installsRepo,
		secretsRepo:  secretsRepo,
		pool:         pool,
		dataDir:      strings.TrimSpace(dataDir),
	}, nil
}

func (s *Service) SyncDesktopMirror(ctx context.Context, accountID uuid.UUID, profileRef string) error {
	if s == nil {
		return nil
	}
	installs, err := s.installsRepo.ListByProfile(ctx, accountID, profileRef)
	if err != nil {
		return err
	}
	serverMap := map[string]any{}
	for _, install := range installs {
		if install.SyncMode != data.MCPSyncModeDesktopFileBidirectional {
			continue
		}
		payload := map[string]any{
			"transport": install.Transport,
		}
		if len(install.LaunchSpecJSON) > 0 {
			var launch map[string]any
			if err := json.Unmarshal(install.LaunchSpecJSON, &launch); err == nil {
				for k, v := range launch {
					payload[k] = v
				}
			}
		}
		if install.AuthHeadersSecretID != nil && s.secretsRepo != nil {
			if plaintext, err := s.secretsRepo.DecryptByID(ctx, *install.AuthHeadersSecretID); err == nil && plaintext != nil {
				var headers map[string]string
				if json.Unmarshal([]byte(*plaintext), &headers) == nil && len(headers) > 0 {
					payload["headers"] = headers
				} else if strings.TrimSpace(*plaintext) != "" {
					payload["bearer_token"] = strings.TrimSpace(*plaintext)
				}
			}
		}
		serverMap[install.InstallKey] = payload
	}
	root := map[string]any{"mcpServers": serverMap}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	path := shareddesktop.MCPServersPath(s.dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	s.recordMTime(path)
	return nil
}

func (s *Service) DiscoverSources(ctx context.Context, req DiscoveryRequest) (DiscoveryResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sources := []DiscoverySource{}
	seen := map[string]struct{}{}
	candidates := []candidateSource{
		{kind: data.MCPSourceKindDesktopFile, path: shareddesktop.MCPServersPath(s.dataDir)},
	}
	if req.WorkspaceRoot != nil && strings.TrimSpace(*req.WorkspaceRoot) != "" {
		candidates = append(candidates, candidateSource{
			kind: data.MCPSourceKindProjectImport,
			path: filepath.Join(strings.TrimSpace(*req.WorkspaceRoot), ".mcp.json"),
		})
	}
	for _, path := range req.Paths {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			continue
		}
		candidates = append(candidates, candidateSource{kind: data.MCPSourceKindExternalImport, path: cleaned})
	}
	for _, candidate := range candidates {
		absPath, err := filepath.Abs(candidate.path)
		if err != nil {
			continue
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		source := DiscoverySource{
			SourceURI:  absPath,
			SourceKind: candidate.kind,
		}
		servers, err := loadMCPServerMap(absPath)
		if err != nil {
			source.ValidationErrors = []string{err.Error()}
			sources = append(sources, source)
			continue
		}
		proposals := make([]ProposedInstall, 0, len(servers))
		installable := true
		for name, raw := range servers {
			proposal, validationErrors, hostWarnings := proposedInstallFromRaw(absPath, candidate.kind, name, raw)
			if len(validationErrors) > 0 {
				source.ValidationErrors = append(source.ValidationErrors, validationErrors...)
				installable = false
				continue
			}
			source.HostWarnings = append(source.HostWarnings, hostWarnings...)
			proposals = append(proposals, proposal)
		}
		source.Installable = installable && len(proposals) > 0
		source.ProposedInstalls = proposals
		sources = append(sources, source)
	}
	return DiscoveryResponse{Sources: sources}, nil
}

func (s *Service) ResolveImport(ctx context.Context, sourceURI string, installKey string) (*ProposedInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanedURI := strings.TrimSpace(sourceURI)
	cleanedKey := strings.TrimSpace(installKey)
	if cleanedURI == "" || cleanedKey == "" {
		return nil, fmt.Errorf("source_uri and install_key are required")
	}
	servers, err := loadMCPServerMap(cleanedURI)
	if err != nil {
		return nil, err
	}
	for name, raw := range servers {
		proposal, validationErrors, _ := proposedInstallFromRaw(cleanedURI, classifySourceKind(cleanedURI, s.dataDir), name, raw)
		if len(validationErrors) > 0 {
			continue
		}
		if proposal.InstallKey == cleanedKey {
			return &proposal, nil
		}
	}
	return nil, fmt.Errorf("install not found in source")
}

func (s *Service) LoadInstallFromSource(ctx context.Context, req ImportRequest) (*ImportedInstall, error) {
	proposal, err := s.ResolveImport(ctx, req.SourceURI, req.InstallKey)
	if err != nil {
		return nil, err
	}
	sourceURI := strings.TrimSpace(req.SourceURI)
	if sourceURI == "" || proposal == nil {
		return nil, fmt.Errorf("install not found in source")
	}
	return &ImportedInstall{
		Install: data.ProfileMCPInstall{
			InstallKey:      proposal.InstallKey,
			DisplayName:     proposal.DisplayName,
			SourceKind:      proposal.SourceKind,
			SourceURI:       &proposal.SourceURI,
			SyncMode:        proposal.SyncMode,
			Transport:       proposal.Transport,
			LaunchSpecJSON:  proposal.LaunchSpecJSON,
			HostRequirement: proposal.HostRequirement,
		},
		AuthHeaders: cloneHeaders(proposal.AuthHeaders),
	}, nil
}

func (s *Service) StartWatcher(ctx context.Context, accountID uuid.UUID, profileRef string, pollInterval time.Duration) {
	if s == nil || pollInterval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.SyncFromOfficialFile(context.Background(), accountID, profileRef)
			}
		}
	}()
}

func (s *Service) SyncFromOfficialFile(ctx context.Context, accountID uuid.UUID, profileRef string) error {
	if s == nil {
		return nil
	}
	path := shareddesktop.MCPServersPath(s.dataDir)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !s.shouldSync(info.ModTime()) {
		return nil
	}
	servers, err := loadMCPServerMap(path)
	if err != nil {
		return err
	}
	existing, err := s.installsRepo.ListByProfile(ctx, accountID, profileRef)
	if err != nil {
		return err
	}
	byKey := make(map[string]data.ProfileMCPInstall, len(existing))
	for _, install := range existing {
		if install.SourceKind != data.MCPSourceKindDesktopFile || install.SyncMode != data.MCPSyncModeDesktopFileBidirectional {
			continue
		}
		byKey[install.InstallKey] = install
	}

	for name, raw := range servers {
		proposal, validationErrors, _ := proposedInstallFromRaw(path, data.MCPSourceKindDesktopFile, name, raw)
		if len(validationErrors) > 0 {
			continue
		}
		now := time.Now().UTC()
		launchJSON := proposal.LaunchSpecJSON
		if current, ok := byKey[proposal.InstallKey]; ok {
			patch := data.MCPInstallPatch{
				DisplayName:      &proposal.DisplayName,
				SourceURI:        &proposal.SourceURI,
				SyncMode:         &proposal.SyncMode,
				Transport:        &proposal.Transport,
				LaunchSpecJSON:   &launchJSON,
				HostRequirement:  &proposal.HostRequirement,
				DiscoveryStatus:  stringPtr(data.MCPDiscoveryStatusNeedsCheck),
				LastErrorCode:    stringPtr(""),
				LastErrorMessage: stringPtr(""),
				LastCheckedAt:    &now,
			}
			headersSecretID, err := s.upsertAuthHeadersSecret(ctx, proposal)
			if err != nil {
				return err
			}
			patch.AuthHeadersSecretID = headersSecretID
			patch.ClearAuthHeaders = headersSecretID == nil
			if _, err := s.installsRepo.Patch(ctx, accountID, current.ID, patch); err != nil {
				return err
			}
			delete(byKey, proposal.InstallKey)
			continue
		}
		install := data.ProfileMCPInstall{
			AccountID:       accountID,
			ProfileRef:      profileRef,
			InstallKey:      proposal.InstallKey,
			DisplayName:     proposal.DisplayName,
			SourceKind:      data.MCPSourceKindDesktopFile,
			SourceURI:       &proposal.SourceURI,
			SyncMode:        data.MCPSyncModeDesktopFileBidirectional,
			Transport:       proposal.Transport,
			LaunchSpecJSON:  launchJSON,
			HostRequirement: proposal.HostRequirement,
			DiscoveryStatus: data.MCPDiscoveryStatusNeedsCheck,
			LastCheckedAt:   &now,
		}
		headersSecretID, err := s.upsertAuthHeadersSecret(ctx, proposal)
		if err != nil {
			return err
		}
		install.AuthHeadersSecretID = headersSecretID
		if _, err := s.installsRepo.Create(ctx, install); err != nil {
			return err
		}
	}
	for _, install := range byKey {
		if s.secretsRepo != nil {
			_ = s.secretsRepo.Delete(ctx, auth.DesktopUserID, "mcp_auth_headers:"+install.InstallKey)
		}
		if err := s.installsRepo.Delete(ctx, accountID, install.ID); err != nil {
			return err
		}
	}
	s.notifyChanged(ctx, accountID)
	s.recordMTime(path)
	return nil
}

type candidateSource struct {
	kind string
	path string
}

func loadMCPServerMap(path string) (map[string]map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("invalid mcp json: %w", err)
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp json root must be an object")
	}
	rawServers, ok := root["mcpServers"]
	if !ok {
		rawServers = root["servers"]
	}
	serverMap, ok := rawServers.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp servers must be an object")
	}
	servers := map[string]map[string]any{}
	for serverName, raw := range serverMap {
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp server %q must be an object", serverName)
		}
		servers[serverName] = obj
	}
	return servers, nil
}

func proposedInstallFromRaw(path, sourceKind, serverName string, raw map[string]any) (ProposedInstall, []string, []string) {
	transport := strings.TrimSpace(asString(raw["transport"]))
	if transport == "" {
		transport = strings.TrimSpace(asString(raw["type"]))
	}
	if transport == "" {
		transport = "stdio"
	}
	launch := map[string]any{}
	hostRequirement := data.MCPHostRequirementRemoteHTTP
	validationErrors := []string{}
	hostWarnings := []string{}

	switch transport {
	case "stdio":
		command := strings.TrimSpace(asString(raw["command"]))
		if command == "" {
			validationErrors = append(validationErrors, fmt.Sprintf("server %q missing command", serverName))
		} else {
			launch["command"] = command
			if _, err := exec.LookPath(command); err != nil {
				hostWarnings = append(hostWarnings, fmt.Sprintf("stdio command %q not found on PATH", command))
			}
		}
		if args, ok := raw["args"].([]any); ok && len(args) > 0 {
			parsed := make([]string, 0, len(args))
			for _, arg := range args {
				if text, ok := arg.(string); ok && strings.TrimSpace(text) != "" {
					parsed = append(parsed, strings.TrimSpace(text))
				}
			}
			launch["args"] = parsed
		}
		if cwd := strings.TrimSpace(asString(raw["cwd"])); cwd != "" {
			launch["cwd"] = cwd
			if _, err := os.Stat(cwd); err != nil {
				hostWarnings = append(hostWarnings, fmt.Sprintf("cwd %q does not exist", cwd))
			}
		}
		if env, ok := raw["env"].(map[string]any); ok && len(env) > 0 {
			parsed := map[string]string{}
			for key, value := range env {
				if strings.TrimSpace(key) == "" {
					continue
				}
				parsed[strings.TrimSpace(key)] = asString(value)
			}
			launch["env"] = parsed
		}
		hostRequirement = data.MCPHostRequirementDesktopLocal
	case "http_sse", "streamable_http":
		url := strings.TrimSpace(asString(raw["url"]))
		if url == "" {
			validationErrors = append(validationErrors, fmt.Sprintf("server %q missing url", serverName))
		} else {
			launch["url"] = url
		}
		hostRequirement = data.MCPHostRequirementRemoteHTTP
	default:
		validationErrors = append(validationErrors, fmt.Sprintf("server %q transport %q not supported", serverName, transport))
	}

	if timeout := intFromAny(raw["call_timeout_ms"]); timeout > 0 {
		launch["call_timeout_ms"] = timeout
	} else if timeout := intFromAny(raw["callTimeoutMs"]); timeout > 0 {
		launch["call_timeout_ms"] = timeout
	}
	authHeaders := map[string]string{}
	if headers, ok := raw["headers"].(map[string]any); ok && len(headers) > 0 {
		for key, value := range headers {
			if strings.TrimSpace(key) == "" {
				continue
			}
			authHeaders[strings.TrimSpace(key)] = asString(value)
		}
	}
	if token := strings.TrimSpace(asString(raw["bearer_token"])); token != "" {
		authHeaders["Authorization"] = "Bearer " + token
	}
	launchJSON, _ := json.Marshal(launch)
	return ProposedInstall{
		InstallKey:      normalizeInstallKey(serverName),
		DisplayName:     strings.TrimSpace(serverName),
		Transport:       transport,
		LaunchSpecJSON:  launchJSON,
		HostRequirement: hostRequirement,
		SourceKind:      sourceKind,
		SourceURI:       path,
		SyncMode:        syncModeForSource(sourceKind),
		HasAuth:         len(authHeaders) > 0,
		AuthHeaders:     authHeaders,
	}, validationErrors, hostWarnings
}

func syncModeForSource(sourceKind string) string {
	if sourceKind == data.MCPSourceKindDesktopFile {
		return data.MCPSyncModeDesktopFileBidirectional
	}
	return data.MCPSyncModeNone
}

func normalizeInstallKey(value string) string {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "_", "-", "_", ".", "_", "/", "_", "\\", "_", ":", "_")
	cleaned = replacer.Replace(cleaned)
	for strings.Contains(cleaned, "__") {
		cleaned = strings.ReplaceAll(cleaned, "__", "_")
	}
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		return "mcp_install"
	}
	return cleaned
}

func (s *Service) shouldSync(modified time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return modified.After(s.lastModified)
}

func (s *Service) recordMTime(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.lastModified = info.ModTime()
	s.mu.Unlock()
}

func (s *Service) upsertAuthHeadersSecret(ctx context.Context, proposal ProposedInstall) (*uuid.UUID, error) {
	if s.secretsRepo == nil {
		return nil, nil
	}
	headers := proposal.AuthHeaders
	if len(headers) == 0 {
		return nil, nil
	}
	encoded, _ := json.Marshal(headers)
	secret, err := s.secretsRepo.Upsert(ctx, auth.DesktopUserID, "mcp_auth_headers:"+proposal.InstallKey, string(encoded))
	if err != nil {
		return nil, err
	}
	return &secret.ID, nil
}

func (s *Service) notifyChanged(ctx context.Context, accountID uuid.UUID) {
	if s == nil || s.pool == nil || accountID == uuid.Nil {
		return
	}
	_, _ = s.pool.Exec(ctx, "SELECT pg_notify('mcp_config_changed', $1)", accountID.String())
}

func cloneHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func classifySourceKind(path string, dataDir string) string {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return data.MCPSourceKindExternalImport
	}
	if cleaned == shareddesktop.MCPServersPath(dataDir) {
		return data.MCPSourceKindDesktopFile
	}
	if strings.HasSuffix(cleaned, ".mcp.json") || strings.HasSuffix(cleaned, string(filepath.Separator)+".vscode"+string(filepath.Separator)+"mcp.json") {
		return data.MCPSourceKindProjectImport
	}
	return data.MCPSourceKindExternalImport
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
