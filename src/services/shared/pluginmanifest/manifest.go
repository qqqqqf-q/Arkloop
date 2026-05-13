package pluginmanifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

var (
	pluginIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	versionLikePattern = regexp.MustCompile(`^v?\d+(?:\.\d+){0,2}(?:[-+][0-9A-Za-z.-]+)?$`)
)

type Manifest struct {
	SchemaVersion   int               `json:"schemaVersion" yaml:"schemaVersion"`
	ID              string            `json:"id" yaml:"id"`
	Name            string            `json:"name,omitempty" yaml:"name,omitempty"`
	Version         string            `json:"version" yaml:"version"`
	Publisher       string            `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	Description     string            `json:"description,omitempty" yaml:"description,omitempty"`
	HostRequirement string            `json:"host_requirement,omitempty" yaml:"host_requirement,omitempty"`
	Platforms       []string          `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Runtime         []RuntimeConfig   `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	MCPServers      []MCPServerConfig `json:"mcp_servers,omitempty" yaml:"mcp_servers,omitempty"`
	Skills          []SkillConfig     `json:"skills,omitempty" yaml:"skills,omitempty"`
	Settings        []SettingConfig   `json:"settings,omitempty" yaml:"settings,omitempty"`
	SettingsSchema  map[string]any    `json:"settings_schema,omitempty" yaml:"settings_schema,omitempty"`
	Hooks           []HookConfig      `json:"hooks,omitempty" yaml:"hooks,omitempty"`
	Context         []ContextConfig   `json:"context,omitempty" yaml:"context,omitempty"`
	Extra           map[string]any    `json:"-" yaml:"-"`
}

type RuntimeConfig struct {
	ID             string                 `json:"id,omitempty" yaml:"id,omitempty"`
	Path           string                 `json:"path,omitempty" yaml:"path,omitempty"`
	Detect         []RuntimeDetectConfig  `json:"detect,omitempty" yaml:"detect,omitempty"`
	VersionCommand []string               `json:"version_command,omitempty" yaml:"version_command,omitempty"`
	VersionMin     string                 `json:"version_min,omitempty" yaml:"version_min,omitempty"`
	Binary         []RuntimeBinaryConfig  `json:"binary,omitempty" yaml:"binary,omitempty"`
	Download       *RuntimeDownloadConfig `json:"download,omitempty" yaml:"download,omitempty"`
	Platforms      []string               `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Arch           []string               `json:"arch,omitempty" yaml:"arch,omitempty"`
}

type RuntimeDetectConfig struct {
	Path           string   `json:"path" yaml:"path"`
	VersionCommand []string `json:"version_command,omitempty" yaml:"version_command,omitempty"`
	VersionMin     string   `json:"version_min,omitempty" yaml:"version_min,omitempty"`
}

type RuntimeDownloadConfig struct {
	URL    string `json:"url" yaml:"url"`
	SHA256 string `json:"sha256" yaml:"sha256"`
}

type RuntimeBinaryConfig struct {
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Platform string `json:"platform,omitempty" yaml:"platform,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	SHA256   string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
}

type MCPServerConfig struct {
	ServerID        string            `json:"server_id" yaml:"server_id"`
	InstallKey      string            `json:"install_key,omitempty" yaml:"install_key,omitempty"`
	DisplayName     string            `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Transport       string            `json:"transport" yaml:"transport"`
	LaunchSpec      map[string]any    `json:"launch_spec,omitempty" yaml:"launch_spec,omitempty"`
	Command         string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args            []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	URL             string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers         map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	WorkingDir      string            `json:"working_dir,omitempty" yaml:"working_dir,omitempty"`
	HostRequirement string            `json:"host_requirement,omitempty" yaml:"host_requirement,omitempty"`
	SourceURI       string            `json:"source_uri,omitempty" yaml:"source_uri,omitempty"`
}

type SkillConfig struct {
	SkillKey string `json:"skill_key" yaml:"skill_key"`
	Bundle   string `json:"bundle,omitempty" yaml:"bundle,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
}

type SettingConfig struct {
	Key      string   `json:"key" yaml:"key"`
	Type     string   `json:"type" yaml:"type"`
	Label    string   `json:"label,omitempty" yaml:"label,omitempty"`
	Required bool     `json:"required,omitempty" yaml:"required,omitempty"`
	Default  any      `json:"default,omitempty" yaml:"default,omitempty"`
	Options  []string `json:"options,omitempty" yaml:"options,omitempty"`
}

type HookConfig struct {
	ID         string            `json:"id,omitempty" yaml:"id,omitempty"`
	Event      string            `json:"event" yaml:"event"`
	Type       string            `json:"type,omitempty" yaml:"type,omitempty"`
	Command    []string          `json:"command,omitempty" yaml:"command,omitempty"`
	Args       []string          `json:"args,omitempty" yaml:"args,omitempty"`
	URL        string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	LaunchSpec map[string]any    `json:"launch_spec,omitempty" yaml:"launch_spec,omitempty"`
	TimeoutMS  int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
}

type ContextConfig struct {
	ID      string `json:"id,omitempty" yaml:"id,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Content string `json:"content,omitempty" yaml:"content,omitempty"`
}

type ManifestJSON []byte

func Parse(data []byte) (Manifest, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Manifest{}, fmt.Errorf("plugin manifest is empty")
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Manifest{}, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if err := rejectUnsupportedFields(root); err != nil {
		return Manifest{}, err
	}
	manifest, err := manifestFromMap(root)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Extra = collectExtra(root)
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ParseJSON(data []byte) (Manifest, error) {
	return Parse(data)
}

func ParseYAML(data []byte) (Manifest, error) {
	return Parse(data)
}

func ToManifestJSON(m Manifest) (ManifestJSON, error) {
	return m.ToManifestJSON()
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("plugin manifest schemaVersion must be %d", SchemaVersion)
	}
	if !pluginIDPattern.MatchString(strings.TrimSpace(m.ID)) {
		return fmt.Errorf("plugin manifest id is invalid")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin manifest version must not be empty")
	}
	if err := validateHostRequirement(m.HostRequirement); err != nil {
		return err
	}
	for _, platform := range m.Platforms {
		if strings.TrimSpace(platform) == "" || strings.ContainsAny(platform, `/\`) {
			return fmt.Errorf("plugin manifest platform is invalid")
		}
	}
	for _, runtime := range m.Runtime {
		if err := runtime.validate(); err != nil {
			return err
		}
	}
	for _, server := range m.MCPServers {
		if err := server.validate(); err != nil {
			return err
		}
	}
	for _, skill := range m.Skills {
		if strings.TrimSpace(skill.SkillKey) == "" {
			return fmt.Errorf("plugin skill key must not be empty")
		}
		path := firstNonEmpty(skill.Bundle, skill.Path)
		if path != "" {
			if err := validateRelativePath(path); err != nil {
				return fmt.Errorf("plugin skill %q path: %w", skill.SkillKey, err)
			}
		}
	}
	for _, setting := range m.Settings {
		if err := setting.validate(); err != nil {
			return err
		}
	}
	for _, hook := range m.Hooks {
		if err := hook.validate(); err != nil {
			return err
		}
	}
	for _, context := range m.Context {
		if strings.TrimSpace(context.Path) == "" && strings.TrimSpace(context.Content) == "" {
			return fmt.Errorf("plugin context must define path or content")
		}
		if strings.TrimSpace(context.Path) != "" {
			if err := validateRelativePath(context.Path); err != nil {
				return fmt.Errorf("plugin context path: %w", err)
			}
		}
	}
	return nil
}

func (m Manifest) ToManifestJSON() (ManifestJSON, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal plugin manifest json: %w", err)
	}
	return ManifestJSON(data), nil
}

func (j ManifestJSON) Manifest() (Manifest, error) {
	return ParseJSON(j)
}

func manifestFromMap(root map[string]any) (Manifest, error) {
	manifest := Manifest{
		SchemaVersion:   intValue(root["schemaVersion"]),
		ID:              firstString(root, "id", "plugin_id"),
		Name:            firstString(root, "name", "display_name"),
		Version:         firstString(root, "version"),
		Publisher:       firstString(root, "publisher"),
		Description:     firstString(root, "description"),
		HostRequirement: hostRequirementString(root["host_requirement"]),
		Platforms:       stringSlice(root["platforms"]),
		Runtime:         runtimeConfigs(root["runtime"]),
		MCPServers:      mcpServerConfigs(root["mcp_servers"]),
		Skills:          skillConfigs(root["skills"]),
		Settings:        settingConfigs(root["settings"]),
		SettingsSchema:  mapAny(root["settings_schema"]),
		Hooks:           hookConfigs(root["hooks"]),
		Context:         contextConfigs(root["context"]),
	}
	if manifest.SettingsSchema == nil && len(manifest.Settings) > 0 {
		manifest.SettingsSchema = settingsSchema(manifest.Settings)
	}
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	for i := range manifest.Runtime {
		runtime := &manifest.Runtime[i]
		if runtime.ID == "" {
			runtime.ID = defaultRuntimeID(*runtime, i)
		}
		for j := range runtime.Detect {
			detect := &runtime.Detect[j]
			if len(detect.VersionCommand) == 0 {
				detect.VersionCommand = append([]string(nil), runtime.VersionCommand...)
			}
			if detect.VersionMin == "" {
				detect.VersionMin = runtime.VersionMin
			}
		}
	}
	for i := range manifest.MCPServers {
		server := &manifest.MCPServers[i]
		if server.ServerID == "" {
			server.ServerID = defaultServerID(*server, i)
		}
		if server.DisplayName == "" {
			server.DisplayName = server.ServerID
		}
		if server.InstallKey == "" {
			server.InstallKey = PluginInstallKey(manifest.ID, server.ServerID)
		}
		if len(server.LaunchSpec) == 0 {
			server.LaunchSpec = serverLaunchSpec(*server)
		}
	}
	for i := range manifest.Skills {
		skill := &manifest.Skills[i]
		if skill.Version == "" {
			skill.Version = manifest.Version
		}
	}
	return manifest, nil
}

func PluginInstallKey(pluginID, serverID string) string {
	return "plugin:" + strings.TrimSpace(pluginID) + ":" + strings.TrimSpace(serverID)
}

func (r RuntimeConfig) validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("plugin runtime id must not be empty")
	}
	if r.Path != "" {
		if err := validateRelativePath(r.Path); err != nil {
			return fmt.Errorf("plugin runtime %q path: %w", r.ID, err)
		}
	}
	for _, binary := range r.Binary {
		if err := binary.validate(r.ID); err != nil {
			return err
		}
	}
	for _, detect := range r.Detect {
		if strings.TrimSpace(detect.Path) == "" {
			return fmt.Errorf("plugin runtime %q detect path must not be empty", r.ID)
		}
		if err := validateRuntimeDetectPath(detect.Path); err != nil {
			return fmt.Errorf("plugin runtime %q detect path: %w", r.ID, err)
		}
		if len(detect.VersionCommand) > 0 && strings.TrimSpace(detect.VersionCommand[0]) == "" {
			return fmt.Errorf("plugin runtime %q version_command command must not be empty", r.ID)
		}
		if strings.TrimSpace(detect.VersionMin) != "" && !versionLikePattern.MatchString(strings.TrimSpace(detect.VersionMin)) {
			return fmt.Errorf("plugin runtime %q version_min is invalid", r.ID)
		}
	}
	if r.Download != nil {
		if strings.TrimSpace(r.Download.URL) == "" {
			return fmt.Errorf("plugin runtime %q download url must not be empty", r.ID)
		}
		if strings.TrimSpace(r.Download.SHA256) == "" {
			return fmt.Errorf("plugin runtime %q download sha256 must not be empty", r.ID)
		}
	}
	return nil
}

func (b RuntimeBinaryConfig) validate(runtimeID string) error {
	if strings.TrimSpace(b.Path) == "" && strings.TrimSpace(b.URL) == "" {
		return fmt.Errorf("plugin runtime %q binary contains empty value", runtimeID)
	}
	if strings.TrimSpace(b.Path) != "" {
		if err := validateRelativePath(b.Path); err != nil {
			return fmt.Errorf("plugin runtime %q binary: %w", runtimeID, err)
		}
	}
	if strings.TrimSpace(b.URL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(b.URL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("plugin runtime %q binary url is invalid", runtimeID)
		}
		if strings.TrimSpace(b.Platform) == "" {
			return fmt.Errorf("plugin runtime %q binary platform must not be empty", runtimeID)
		}
		if strings.TrimSpace(b.SHA256) == "" {
			return fmt.Errorf("plugin runtime %q binary sha256 must not be empty", runtimeID)
		}
	}
	return nil
}

func (s MCPServerConfig) validate() error {
	if strings.TrimSpace(s.ServerID) == "" {
		return fmt.Errorf("plugin mcp server id must not be empty")
	}
	transport := normalizeTransport(s.Transport)
	switch transport {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" && strings.TrimSpace(stringFromMap(s.LaunchSpec, "command")) == "" {
			return fmt.Errorf("plugin mcp server %q command must not be empty", s.ServerID)
		}
		if s.Command != "" {
			if err := validatePathLikeCommand(s.Command); err != nil {
				return fmt.Errorf("plugin mcp server %q command: %w", s.ServerID, err)
			}
		}
		if s.WorkingDir != "" {
			if err := validateRelativePath(s.WorkingDir); err != nil {
				return fmt.Errorf("plugin mcp server %q working_dir: %w", s.ServerID, err)
			}
		}
	case "http_sse", "streamable_http":
		rawURL := firstNonEmpty(s.URL, stringFromMap(s.LaunchSpec, "url"))
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("plugin mcp server %q url is invalid", s.ServerID)
		}
	default:
		return fmt.Errorf("plugin mcp server %q transport is invalid", s.ServerID)
	}
	return nil
}

func (s SettingConfig) validate() error {
	if strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("plugin setting key must not be empty")
	}
	settingType := strings.TrimSpace(s.Type)
	switch settingType {
	case "string", "text", "password", "path", "url", "select", "number", "integer", "boolean", "array", "object":
	default:
		return fmt.Errorf("plugin setting %q type is invalid", s.Key)
	}
	if s.Default != nil && !defaultMatchesType(s.Default, settingType) {
		return fmt.Errorf("plugin setting %q default does not match type %q", s.Key, settingType)
	}
	if settingType == "select" && s.Default != nil && len(s.Options) > 0 {
		defaultValue, _ := s.Default.(string)
		if !slices.Contains(s.Options, defaultValue) {
			return fmt.Errorf("plugin setting %q default is not in options", s.Key)
		}
	}
	return nil
}

func defaultMatchesType(value any, settingType string) bool {
	switch settingType {
	case "string", "text", "password", "path", "url", "select":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case int, int64, float64, float32:
			return true
		default:
			return false
		}
	case "integer":
		switch typed := value.(type) {
		case int, int64:
			return true
		case float64:
			return typed == float64(int64(typed))
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return false
	}
}

func (h HookConfig) validate() error {
	if normalizeHookEvent(h.Event) == "" {
		return fmt.Errorf("plugin hook event %q is invalid", h.Event)
	}
	hookType := strings.TrimSpace(h.Type)
	if hookType == "" {
		if len(h.Command) > 0 {
			hookType = "command"
		} else if strings.TrimSpace(h.URL) != "" {
			hookType = "http"
		}
	}
	switch hookType {
	case "":
		return fmt.Errorf("plugin hook handler must not be empty")
	case "command":
		if len(h.Command) == 0 || strings.TrimSpace(h.Command[0]) == "" {
			return fmt.Errorf("plugin hook command must not be empty")
		}
		if err := validatePathLikeCommand(h.Command[0]); err != nil {
			return fmt.Errorf("plugin hook command: %w", err)
		}
	case "http":
		parsed, err := url.Parse(strings.TrimSpace(h.URL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("plugin hook url is invalid")
		}
	default:
		return fmt.Errorf("plugin hook type %q is invalid", h.Type)
	}
	return nil
}

func runtimeConfigs(value any) []RuntimeConfig {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]RuntimeConfig, 0, len(typed))
		for _, item := range typed {
			if runtime, ok := runtimeConfig(item); ok {
				out = append(out, runtime)
			}
		}
		return out
	default:
		if runtime, ok := runtimeConfig(typed); ok {
			return []RuntimeConfig{runtime}
		}
		return nil
	}
}

func runtimeConfig(value any) (RuntimeConfig, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		return RuntimeConfig{}, false
	}
	runtime := RuntimeConfig{
		ID:             firstString(raw, "id", "runtime_id", "name"),
		Path:           firstString(raw, "path"),
		Detect:         runtimeDetectConfigs(raw["detect"], stringSlice(raw["version_command"]), firstString(raw, "version_min")),
		VersionCommand: stringSlice(raw["version_command"]),
		VersionMin:     firstString(raw, "version_min"),
		Binary:         runtimeBinaryConfigs(raw["binary"]),
		Platforms:      stringSlice(raw["platforms"]),
		Arch:           stringSlice(raw["arch"]),
	}
	if download := mapAny(raw["download"]); download != nil {
		runtime.Download = &RuntimeDownloadConfig{
			URL:    firstString(download, "url"),
			SHA256: firstString(download, "sha256"),
		}
	}
	return runtime, true
}

func runtimeDetectConfigs(value any, inheritedCommand []string, inheritedMin string) []RuntimeDetectConfig {
	switch typed := value.(type) {
	case string:
		return []RuntimeDetectConfig{{Path: strings.TrimSpace(typed), VersionCommand: inheritedCommand, VersionMin: inheritedMin}}
	case []any:
		out := make([]RuntimeDetectConfig, 0, len(typed))
		for _, item := range typed {
			switch raw := item.(type) {
			case string:
				out = append(out, RuntimeDetectConfig{Path: strings.TrimSpace(raw), VersionCommand: inheritedCommand, VersionMin: inheritedMin})
			case map[string]any:
				out = append(out, RuntimeDetectConfig{
					Path:           firstString(raw, "path", "command"),
					VersionCommand: firstNonEmptyStringSlice(stringSlice(raw["version_command"]), inheritedCommand),
					VersionMin:     firstNonEmpty(firstString(raw, "version_min"), inheritedMin),
				})
			}
		}
		return out
	case map[string]any:
		return []RuntimeDetectConfig{{
			Path:           firstString(typed, "path", "command"),
			VersionCommand: firstNonEmptyStringSlice(stringSlice(typed["version_command"]), inheritedCommand),
			VersionMin:     firstNonEmpty(firstString(typed, "version_min"), inheritedMin),
		}}
	default:
		return nil
	}
}

func runtimeBinaryConfigs(value any) []RuntimeBinaryConfig {
	switch typed := value.(type) {
	case string:
		path := strings.TrimSpace(typed)
		if path == "" {
			return nil
		}
		return []RuntimeBinaryConfig{{Path: path}}
	case []any:
		out := make([]RuntimeBinaryConfig, 0, len(typed))
		for _, item := range typed {
			if binary, ok := runtimeBinaryConfig(item); ok {
				out = append(out, binary)
			}
		}
		return out
	case map[string]any:
		if binary, ok := runtimeBinaryConfig(typed); ok {
			return []RuntimeBinaryConfig{binary}
		}
		return nil
	default:
		return nil
	}
}

func runtimeBinaryConfig(value any) (RuntimeBinaryConfig, bool) {
	switch typed := value.(type) {
	case string:
		path := strings.TrimSpace(typed)
		if path == "" {
			return RuntimeBinaryConfig{}, false
		}
		return RuntimeBinaryConfig{Path: path}, true
	case map[string]any:
		binary := RuntimeBinaryConfig{
			Path:     firstString(typed, "path"),
			Platform: firstString(typed, "platform"),
			URL:      firstString(typed, "url"),
			SHA256:   firstString(typed, "sha256"),
		}
		return binary, binary.Path != "" || binary.URL != ""
	default:
		return RuntimeBinaryConfig{}, false
	}
}

func mcpServerConfigs(value any) []MCPServerConfig {
	switch typed := value.(type) {
	case []any:
		out := make([]MCPServerConfig, 0, len(typed))
		for _, item := range typed {
			if server, ok := mcpServerConfig("", item); ok {
				out = append(out, server)
			}
		}
		return out
	case map[string]any:
		if looksLikeServerConfig(typed) {
			if server, ok := mcpServerConfig("", typed); ok {
				return []MCPServerConfig{server}
			}
			return nil
		}
		out := make([]MCPServerConfig, 0, len(typed))
		keys := sortedKeys(typed)
		for _, key := range keys {
			if server, ok := mcpServerConfig(key, typed[key]); ok {
				out = append(out, server)
			}
		}
		return out
	default:
		return nil
	}
}

func mcpServerConfig(fallbackID string, value any) (MCPServerConfig, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		return MCPServerConfig{}, false
	}
	transport := normalizeTransport(firstString(raw, "transport", "type"))
	server := MCPServerConfig{
		ServerID:        firstNonEmpty(firstString(raw, "server_id", "id"), fallbackID, serverIDFromInstallKey(firstString(raw, "install_key"))),
		InstallKey:      firstString(raw, "install_key"),
		DisplayName:     firstString(raw, "display_name", "name"),
		Transport:       transport,
		LaunchSpec:      mapAny(firstNonNil(raw["launch_spec"], raw["launchSpec"])),
		Command:         firstString(raw, "command"),
		Args:            stringSlice(raw["args"]),
		Env:             stringMap(raw["env"]),
		URL:             firstString(raw, "url"),
		Headers:         stringMap(raw["headers"]),
		WorkingDir:      firstString(raw, "working_dir", "cwd"),
		HostRequirement: hostRequirementString(raw["host_requirement"]),
		SourceURI:       firstString(raw, "source_uri"),
	}
	if server.Transport == "" {
		server.Transport = normalizeTransport(stringFromMap(server.LaunchSpec, "transport"))
	}
	if server.Transport == "" && server.URL != "" {
		server.Transport = "streamable_http"
	}
	return server, true
}

func skillConfigs(value any) []SkillConfig {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]SkillConfig, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := firstString(raw, "path")
		bundle := firstString(raw, "bundle")
		if path == "" {
			path = bundle
		}
		out = append(out, SkillConfig{
			SkillKey: firstString(raw, "skill_key", "id"),
			Bundle:   bundle,
			Path:     path,
			Version:  firstString(raw, "version"),
		})
	}
	return out
}

func settingConfigs(value any) []SettingConfig {
	switch typed := value.(type) {
	case []any:
		out := make([]SettingConfig, 0, len(typed))
		for _, item := range typed {
			raw, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, settingConfig(raw))
		}
		return out
	case map[string]any:
		out := make([]SettingConfig, 0, len(typed))
		for _, key := range sortedKeys(typed) {
			raw, ok := typed[key].(map[string]any)
			if !ok {
				continue
			}
			setting := settingConfig(raw)
			if setting.Key == "" {
				setting.Key = key
			}
			out = append(out, setting)
		}
		return out
	default:
		return nil
	}
}

func settingConfig(raw map[string]any) SettingConfig {
	return SettingConfig{
		Key:      firstString(raw, "key"),
		Type:     firstString(raw, "type"),
		Label:    firstString(raw, "label"),
		Required: boolValue(raw["required"]),
		Default:  raw["default"],
		Options:  stringSlice(raw["options"]),
	}
}

func hookConfigs(value any) []HookConfig {
	switch typed := value.(type) {
	case []any:
		out := make([]HookConfig, 0, len(typed))
		for _, item := range typed {
			raw, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, hookConfig("", raw))
		}
		return out
	case map[string]any:
		if looksLikeHookConfig(typed) {
			return []HookConfig{hookConfig("", typed)}
		}
		out := make([]HookConfig, 0, len(typed))
		for _, event := range sortedKeys(typed) {
			raw, ok := typed[event].(map[string]any)
			if !ok {
				continue
			}
			out = append(out, hookConfig(event, raw))
		}
		return out
	default:
		return nil
	}
}

func hookConfig(fallbackEvent string, raw map[string]any) HookConfig {
	launchSpec := mapAny(firstNonNil(raw["launch_spec"], raw["launchSpec"]))
	command := stringSlice(raw["command"])
	args := stringSlice(raw["args"])
	url := firstString(raw, "url")
	headers := stringMap(raw["headers"])
	hookType := firstString(raw, "type", "runtime_type")
	if hookType == "" {
		hookType = stringFromMap(launchSpec, "type")
	}
	if len(command) == 0 {
		command = stringSlice(launchSpec["command"])
	}
	if len(args) == 0 {
		args = stringSlice(launchSpec["args"])
	}
	if url == "" {
		url = stringFromMap(launchSpec, "url")
	}
	if len(headers) == 0 {
		headers = stringMap(launchSpec["headers"])
	}
	if hookType == "" {
		switch {
		case len(command) > 0:
			hookType = "command"
		case url != "":
			hookType = "http"
		}
	}
	return HookConfig{
		ID:         firstString(raw, "id", "hook_id"),
		Event:      normalizeHookEvent(firstNonEmpty(firstString(raw, "event", "hook", "name"), fallbackEvent)),
		Type:       hookType,
		Command:    command,
		Args:       args,
		URL:        url,
		Headers:    headers,
		LaunchSpec: launchSpec,
		TimeoutMS:  intValue(firstNonNil(raw["timeout_ms"], raw["timeoutMs"])),
	}
}

func contextConfigs(value any) []ContextConfig {
	switch typed := value.(type) {
	case string:
		path := strings.TrimSpace(typed)
		if path == "" {
			return nil
		}
		return []ContextConfig{{ID: contextID(path, 0), Path: path}}
	case []any:
		out := make([]ContextConfig, 0, len(typed))
		for index, item := range typed {
			out = append(out, contextConfig(item, index)...)
		}
		return out
	case map[string]any:
		return contextConfig(typed, 0)
	default:
		return nil
	}
}

func contextConfig(value any, index int) []ContextConfig {
	switch typed := value.(type) {
	case string:
		path := strings.TrimSpace(typed)
		if path == "" {
			return nil
		}
		return []ContextConfig{{ID: contextID(path, index), Path: path}}
	case map[string]any:
		if path := firstString(typed, "path"); path != "" {
			return []ContextConfig{{
				ID:      firstNonEmpty(firstString(typed, "id"), contextID(path, index)),
				Path:    path,
				Content: firstString(typed, "content", "text", "body"),
			}}
		}
		if content := firstString(typed, "content", "text", "body"); content != "" {
			return []ContextConfig{{ID: firstNonEmpty(firstString(typed, "id"), fmt.Sprintf("context_%d", index+1)), Content: content}}
		}
		out := make([]ContextConfig, 0, len(typed))
		for _, key := range sortedKeys(typed) {
			path := stringValue(typed[key])
			if path != "" {
				out = append(out, ContextConfig{ID: key, Path: path})
			}
		}
		return out
	default:
		return nil
	}
}

func serverLaunchSpec(server MCPServerConfig) map[string]any {
	spec := map[string]any{"transport": server.Transport}
	switch server.Transport {
	case "stdio":
		spec["command"] = server.Command
		if len(server.Args) > 0 {
			spec["args"] = append([]string(nil), server.Args...)
		}
		if len(server.Env) > 0 {
			spec["env"] = copyStringMap(server.Env)
		}
		if server.WorkingDir != "" {
			spec["cwd"] = server.WorkingDir
		}
	case "http_sse", "streamable_http":
		spec["url"] = server.URL
		if len(server.Headers) > 0 {
			spec["headers"] = copyStringMap(server.Headers)
		}
	}
	return spec
}

func normalizeHookEvent(event string) string {
	event = strings.TrimSpace(event)
	if event == "" {
		return ""
	}
	var out strings.Builder
	for i, r := range event {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		switch r {
		case '-', '.', ' ':
			out.WriteByte('_')
		default:
			out.WriteRune(r)
		}
	}
	switch strings.ToLower(strings.Trim(out.String(), "_")) {
	case "before_tool_use", "before_tool":
		return "BeforeToolUse"
	case "after_tool_use", "after_tool":
		return "AfterToolUse"
	case "before_model", "before_model_call":
		return "BeforeModel"
	case "after_model", "after_model_response":
		return "AfterModel"
	case "session_start", "before_run":
		return "SessionStart"
	case "session_end", "after_run":
		return "SessionEnd"
	default:
		return ""
	}
}

func validateRelativePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("must not be empty")
	}
	if isAbsolutePathLike(path) {
		return fmt.Errorf("must be relative")
	}
	slashPath := strings.ReplaceAll(path, "\\", "/")
	if hasParentPathSegment(slashPath) {
		return fmt.Errorf("must not escape plugin root")
	}
	cleaned := pathpkg.Clean(slashPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("must not escape plugin root")
	}
	return nil
}

func validatePathLikeCommand(command string) error {
	command = strings.TrimSpace(command)
	if !strings.ContainsAny(command, `/\`) && !strings.HasPrefix(command, ".") {
		return nil
	}
	return validateRelativePath(command)
}

func validateRuntimeDetectPath(path string) error {
	path = strings.TrimSpace(path)
	if isAbsolutePathLike(path) {
		return nil
	}
	return validatePathLikeCommand(path)
}

func isAbsolutePathLike(path string) bool {
	path = strings.TrimSpace(path)
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return true
	}
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func hasParentPathSegment(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func validateHostRequirement(requirement string) error {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return nil
	}
	if strings.ContainsAny(requirement, `/\`) || strings.Contains(requirement, "..") {
		return fmt.Errorf("plugin host_requirement is invalid")
	}
	return nil
}

func rejectUnsupportedFields(root map[string]any) error {
	for _, key := range []string{"commands", "agents", "toolPolicy", "webview"} {
		if _, ok := root[key]; ok {
			return fmt.Errorf("plugin manifest field %q is not supported", key)
		}
	}
	return nil
}

func collectExtra(root map[string]any) map[string]any {
	known := map[string]struct{}{
		"schemaVersion": {}, "id": {}, "plugin_id": {}, "name": {}, "display_name": {},
		"version": {}, "publisher": {}, "description": {}, "host_requirement": {},
		"platforms": {}, "runtime": {}, "mcp_servers": {}, "skills": {}, "settings": {},
		"settings_schema": {}, "hooks": {}, "context": {},
	}
	extra := make(map[string]any)
	for key, value := range root {
		if _, ok := known[key]; !ok {
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func hostRequirementString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		minVersion := firstString(typed, "min_version")
		if minVersion != "" {
			return ">=" + minVersion
		}
		return firstString(typed, "value")
	default:
		return ""
	}
}

func normalizeTransport(value string) string {
	switch strings.TrimSpace(value) {
	case "http":
		return "streamable_http"
	default:
		return strings.TrimSpace(value)
	}
}

func looksLikeServerConfig(raw map[string]any) bool {
	for _, key := range []string{"server_id", "id", "install_key", "transport", "type", "command", "url", "launch_spec", "launchSpec"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

func looksLikeHookConfig(raw map[string]any) bool {
	for _, key := range []string{"event", "hook", "name", "type", "command", "args", "url", "launch_spec", "launchSpec"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

func defaultRuntimeID(runtime RuntimeConfig, index int) string {
	for _, candidate := range []string{runtime.Path, firstDetectPath(runtime), firstBinaryCandidate(runtime.Binary)} {
		if id := idFromText(candidate); id != "" {
			return id
		}
	}
	return fmt.Sprintf("runtime_%d", index+1)
}

func defaultServerID(server MCPServerConfig, index int) string {
	for _, candidate := range []string{server.Command, server.URL, server.DisplayName} {
		if id := idFromText(candidate); id != "" {
			return id
		}
	}
	return fmt.Sprintf("server_%d", index+1)
}

func contextID(path string, index int) string {
	if id := idFromText(path); id != "" {
		return id
	}
	return fmt.Sprintf("context_%d", index+1)
}

func idFromText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(filepath.ToSlash(value))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.Trim(base, "._-:")
	if base == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '.', r == '_', r == '-', r == ':':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return strings.Trim(out.String(), "._-:")
}

func firstDetectPath(runtime RuntimeConfig) string {
	if len(runtime.Detect) == 0 {
		return ""
	}
	return runtime.Detect[0].Path
}

func firstBinaryCandidate(values []RuntimeBinaryConfig) string {
	if len(values) == 0 {
		return ""
	}
	if strings.TrimSpace(values[0].Path) != "" {
		return values[0].Path
	}
	return values[0].URL
}

func settingsSchema(settings []SettingConfig) map[string]any {
	out := make(map[string]any, len(settings))
	for _, setting := range settings {
		key := strings.TrimSpace(setting.Key)
		if key == "" {
			continue
		}
		item := map[string]any{"type": setting.Type}
		if setting.Label != "" {
			item["label"] = setting.Label
		}
		if setting.Default != nil {
			item["default"] = setting.Default
		}
		if len(setting.Options) > 0 {
			item["options"] = append([]string(nil), setting.Options...)
		}
		out[key] = item
	}
	return out
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(raw[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return append([]string(nil), value...)
		}
	}
	return nil
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case uint64:
		return int(typed)
	default:
		return 0
	}
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case string:
		value := strings.TrimSpace(typed)
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			value := stringValue(item)
			if value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func stringMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = stringValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapAny(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	return out
}

func stringFromMap(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	return stringValue(raw[key])
}

func copyStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func serverIDFromInstallKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ":")
	if len(parts) == 0 {
		return value
	}
	return strings.TrimSpace(parts[len(parts)-1])
}
