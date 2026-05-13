package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"arkloop/services/shared/pluginhook"
	sharedpluginmanifest "arkloop/services/shared/pluginmanifest"
)

func NewPluginHooksMiddleware(queryer pluginQueryer) RunMiddleware {
	return NewPluginHooksMiddlewareWithLoader(func(ctx context.Context, rc *RunContext) ([]PluginHookConfig, error) {
		return LoadPluginHooks(ctx, queryer, rc)
	})
}

func NewPluginHooksMiddlewareWithLoader(loader PluginHooksLoader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		rc.PluginHooks = nil
		if loader == nil || rc == nil {
			return next(ctx, rc)
		}
		hooks, err := loader(ctx, rc)
		if err != nil {
			tracePluginHooks(rc, "failed", 0, err.Error())
			return err
		}
		rc.PluginHooks = append([]PluginHookConfig(nil), hooks...)
		tracePluginHooks(rc, "completed", len(rc.PluginHooks), "")
		return next(ctx, rc)
	}
}

func LoadPluginHooks(ctx context.Context, queryer pluginQueryer, rc *RunContext) ([]PluginHookConfig, error) {
	records, err := loadPluginEnablements(ctx, queryer, rc)
	if err != nil {
		if isPluginSchemaUnavailable(err) {
			tracePluginHooks(rc, "skipped", 0, err.Error())
			return nil, nil
		}
		return nil, err
	}
	hooks := make([]PluginHookConfig, 0, len(records))
	for _, record := range records {
		settings := decodePluginMap(record.SettingsJSON)
		runtimeState := decodePluginMap(record.RuntimeStateJSON)
		pluginHooks, err := pluginManifestHooks(record.PluginID, record.ManifestJSON, settings, runtimeState)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, pluginHooks...)
	}
	return hooks, nil
}

func pluginManifestHooks(pluginID string, raw json.RawMessage, settings map[string]any, runtimeState map[string]any) ([]PluginHookConfig, error) {
	manifest := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &manifest) != nil {
		return nil, nil
	}
	return pluginHookConfigsFromValue(pluginID, manifest["hooks"], settings, runtimeState)
}

func pluginHookConfigsFromValue(pluginID string, value any, settings map[string]any, runtimeState map[string]any) ([]PluginHookConfig, error) {
	switch typed := value.(type) {
	case []any:
		out := make([]PluginHookConfig, 0, len(typed))
		for idx, item := range typed {
			if raw, ok := item.(map[string]any); ok {
				hook, ok, err := pluginHookConfigFromMap(pluginID, fmt.Sprintf("hook_%d", idx+1), raw, settings, runtimeState)
				if err != nil {
					return nil, err
				}
				if ok {
					out = append(out, hook)
				}
			}
		}
		return out, nil
	case map[string]any:
		out := make([]PluginHookConfig, 0, len(typed))
		for event, item := range typed {
			switch raw := item.(type) {
			case map[string]any:
				raw["event"] = event
				hook, ok, err := pluginHookConfigFromMap(pluginID, event, raw, settings, runtimeState)
				if err != nil {
					return nil, err
				}
				if ok {
					out = append(out, hook)
				}
			case []any:
				for idx, nested := range raw {
					if rawMap, ok := nested.(map[string]any); ok {
						rawMap["event"] = event
						hook, ok, err := pluginHookConfigFromMap(pluginID, fmt.Sprintf("%s_%d", event, idx+1), rawMap, settings, runtimeState)
						if err != nil {
							return nil, err
						}
						if ok {
							out = append(out, hook)
						}
					}
				}
			}
		}
		return out, nil
	default:
		return nil, nil
	}
}

func pluginHookConfigFromMap(pluginID string, fallbackID string, raw map[string]any, settings map[string]any, runtimeState map[string]any) (PluginHookConfig, bool, error) {
	event := normalizePluginHookEvent(firstString(raw, "event", "hook", "name"))
	if event == "" {
		return PluginHookConfig{}, false, nil
	}
	hookID := strings.TrimSpace(firstString(raw, "id", "hook_id"))
	if hookID == "" {
		hookID = fallbackID
	}
	launchSpec := firstMap(raw, "launch_spec", "launchSpec")
	if len(launchSpec) == 0 {
		launchSpec = firstMap(raw, "runtime", "runtime_config")
	}
	renderedLaunchSpec, err := renderPluginPlaceholders(launchSpec, settings, runtimeState)
	if err != nil {
		return PluginHookConfig{}, false, err
	}
	hookType := firstNonEmptyPluginString(firstString(raw, "type", "runtime_type"), firstString(renderedLaunchSpec, "type"))
	command := firstStringSlice(raw["command"])
	if len(command) == 0 {
		command = firstStringSlice(renderedLaunchSpec["command"])
	}
	args := firstStringSlice(raw["args"])
	if len(args) == 0 {
		args = firstStringSlice(renderedLaunchSpec["args"])
	}
	url := firstNonEmptyPluginString(firstString(raw, "url"), firstString(renderedLaunchSpec, "url"))
	headers := firstStringMap(raw["headers"])
	if len(headers) == 0 {
		headers = firstStringMap(renderedLaunchSpec["headers"])
	}
	if hookType == "" {
		switch {
		case len(command) > 0:
			hookType = string(pluginhook.HookTypeCommand)
		case url != "":
			hookType = string(pluginhook.HookTypeHTTP)
		}
	}
	hook := PluginHookConfig{
		PluginID:     strings.TrimSpace(pluginID),
		HookID:       sanitizePromptSegmentName(hookID),
		Event:        event,
		Runtime:      hookType,
		LaunchSpec:   renderedLaunchSpec,
		Settings:     copyPluginMap(settings),
		RuntimeState: copyPluginMap(runtimeState),
		Timeout:      pluginHookTimeout(raw),
	}
	hook.HookConfig = pluginhook.HookConfig{
		PluginID:   hook.PluginID,
		PluginData: firstPluginString(runtimeState, "plugin_data", "pluginData", "data_dir", "dataDir"),
		Event:      pluginhook.HookEvent(toSharedPluginHookEvent(event)),
		Type:       pluginhook.HookType(hookType),
		TimeoutMS:  pluginHookTimeoutMS(raw),
	}
	hook.HookConfig.Command, err = renderPluginStringSlice(command, settings, runtimeState)
	if err != nil {
		return PluginHookConfig{}, false, err
	}
	hook.HookConfig.Args, err = renderPluginStringSlice(args, settings, runtimeState)
	if err != nil {
		return PluginHookConfig{}, false, err
	}
	hook.HookConfig.URL, err = renderPluginString(url, settings, runtimeState)
	if err != nil {
		return PluginHookConfig{}, false, err
	}
	hook.HookConfig.Headers, err = renderPluginStringMap(headers, settings, runtimeState)
	if err != nil {
		return PluginHookConfig{}, false, err
	}
	return hook, hook.PluginID != "" && hook.HookID != "", nil
}

func pluginHookTimeout(raw map[string]any) time.Duration {
	ms := pluginHookTimeoutMS(raw)
	if ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

func pluginHookTimeoutMS(raw map[string]any) int {
	for _, key := range []string{"timeout_ms", "timeoutMs"} {
		switch value := raw[key].(type) {
		case float64:
			if value > 0 {
				return int(value)
			}
		case int:
			if value > 0 {
				return value
			}
		}
	}
	return 0
}

func renderPluginPlaceholders(src map[string]any, settings map[string]any, runtimeState map[string]any) (map[string]any, error) {
	if len(src) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		rendered, err := renderPluginValue(value, settings, runtimeState)
		if err != nil {
			return nil, err
		}
		out[key] = rendered
	}
	return out, nil
}

func renderPluginValue(value any, settings map[string]any, runtimeState map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		return renderPluginString(typed, settings, runtimeState)
	case map[string]any:
		return renderPluginPlaceholders(typed, settings, runtimeState)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			rendered, err := renderPluginValue(item, settings, runtimeState)
			if err != nil {
				return nil, err
			}
			out[i] = rendered
		}
		return out, nil
	default:
		return value, nil
	}
}

func renderPluginString(value string, settings map[string]any, runtimeState map[string]any) (string, error) {
	return sharedpluginmanifest.ResolveString(value, sharedpluginmanifest.PlaceholderContext{
		Settings:     pluginStringSettings(settings),
		RuntimePaths: pluginRuntimePaths(runtimeState),
		PluginData:   firstPluginString(runtimeState, "plugin_data", "pluginData", "data_dir", "dataDir"),
		Arch:         runtime.GOARCH,
		Platform:     runtime.GOOS,
	})
}

func renderPluginStringSlice(values []string, settings map[string]any, runtimeState map[string]any) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		rendered, err := renderPluginString(value, settings, runtimeState)
		if err != nil {
			return nil, err
		}
		out[i] = rendered
	}
	return out, nil
}

func renderPluginStringMap(values map[string]string, settings map[string]any, runtimeState map[string]any) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		rendered, err := renderPluginString(value, settings, runtimeState)
		if err != nil {
			return nil, err
		}
		out[key] = rendered
	}
	return out, nil
}

func pluginStringSettings(settings map[string]any) map[string]string {
	if len(settings) == 0 {
		return nil
	}
	out := make(map[string]string, len(settings))
	for key, value := range settings {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = fmt.Sprint(value)
		}
	}
	return out
}

func pluginRuntimePaths(runtimeState map[string]any) map[string]string {
	if len(runtimeState) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range runtimeState {
		key = strings.TrimSpace(key)
		if strings.HasSuffix(key, ".path") {
			runtimeID := strings.TrimSuffix(key, ".path")
			if runtimeID != "" {
				out[runtimeID] = fmt.Sprint(value)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodePluginMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstMap(raw map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := raw[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func firstStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func firstStringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			out[key] = fmt.Sprint(item)
		}
		return out
	default:
		return nil
	}
}

func firstPluginString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := values[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func tracePluginHooks(rc *RunContext, status string, count int, err string) {
	fields := map[string]any{
		"status":     strings.TrimSpace(status),
		"hook_count": count,
	}
	if strings.TrimSpace(err) != "" {
		fields["error"] = strings.TrimSpace(err)
	}
	emitTraceEvent(rc, "plugin_hooks", "plugin_hooks."+strings.TrimSpace(status), fields)
}
