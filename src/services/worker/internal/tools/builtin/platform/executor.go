package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"arkloop/services/worker/internal/tools"
)

const (
	errArgsInvalid      = "tool.args_invalid"
	errAPICall          = "tool.api_call_failed"
	errHTTP             = "tool.http_error"
	errUnauthorized     = "tool.unauthorized"
	errBridgeUnavailable = "tool.bridge_unavailable"
)

// Executor 是 platform_manage 工具的统一执行器。
// 通过 action 参数路由到对应的 API / Bridge 调用。
type Executor struct {
	http       *http.Client
	apiBase    string
	bridgeBase string
	tp         *TokenProvider
}

func NewExecutor(apiBaseURL, bridgeBaseURL string, tp *TokenProvider) *Executor {
	return &Executor{
		http:       sharedoutbound.DefaultPolicy().NewInternalHTTPClient(30 * time.Second),
		apiBase:    strings.TrimRight(apiBaseURL, "/"),
		bridgeBase: strings.TrimRight(bridgeBaseURL, "/"),
		tp:         tp,
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	t := time.Now()
	action, _ := args["action"].(string)
	if action == "" {
		return argErr("action is required", t)
	}
	// params object -> flat map for action methods
	if p, ok := args["params"].(map[string]any); ok {
		flat := make(map[string]any, len(p)+1)
		for k, v := range p {
			flat[k] = v
		}
		flat["action"] = action
		args = flat
	}

	switch action {
	// --- settings ---
	case "get_settings":
		return e.get(ctx, "/v1/admin/platform-settings", t)
	case "set_setting":
		return e.setSetting(ctx, args, t)
	case "configure_email":
		return e.configureEmail(ctx, args, t)
	case "test_email":
		return e.testEmail(ctx, args, t)
	case "configure_smtp":
		return e.configureSMTP(ctx, args, t)
	case "configure_captcha":
		return e.configureCaptcha(ctx, args, t)
	case "configure_registration":
		return e.configureRegistration(ctx, args, t)
	case "configure_gateway":
		return e.configureGateway(ctx, args, t)
	case "update_styles":
		return e.updateStyles(ctx, args, t)

	// --- providers ---
	case "list_providers":
		return e.get(ctx, "/v1/llm-providers", t)
	case "add_provider":
		return e.addProvider(ctx, args, t)
	case "update_provider":
		return e.updateProvider(ctx, args, t)
	case "delete_provider":
		return e.requireDelete(ctx, args, "/v1/llm-providers/", t)
	case "list_models":
		return e.listModels(ctx, args, t)
	case "configure_model":
		return e.configureModel(ctx, args, t)

	// --- agents ---
	case "list_agents":
		return e.get(ctx, "/v1/personas", t)
	case "create_agent":
		return e.createAgent(ctx, args, t)
	case "update_agent":
		return e.updateAgent(ctx, args, t)
	case "delete_agent":
		return e.requireDelete(ctx, args, "/v1/personas/", t)
	case "get_agent":
		return e.requireGet(ctx, args, "/v1/personas/", t)

	// --- skills ---
	case "list_skills":
		return e.get(ctx, "/v1/skill-packages", t)
	case "install_skill_market":
		return e.installSkillMarket(ctx, args, t)
	case "install_skill_github":
		return e.installSkillGithub(ctx, args, t)
	case "remove_skill":
		return e.requireDelete(ctx, args, "/v1/skill-packages/", t)

	// --- mcp ---
	case "list_mcp_configs":
		return e.get(ctx, "/v1/mcp-configs", t)
	case "add_mcp_config":
		return e.addMCPConfig(ctx, args, t)
	case "update_mcp_config":
		return e.updateMCPConfig(ctx, args, t)
	case "delete_mcp_config":
		return e.requireDelete(ctx, args, "/v1/mcp-configs/", t)
	case "list_tool_providers":
		return e.get(ctx, "/v1/tool-providers", t)
	case "add_tool_provider":
		return e.addToolProvider(ctx, args, t)
	case "update_tool_provider":
		return e.updateToolProvider(ctx, args, t)

	// --- access ---
	case "list_ip_rules":
		return e.get(ctx, "/v1/ip-rules", t)
	case "add_ip_rule":
		return e.addIPRule(ctx, args, t)
	case "delete_ip_rule":
		return e.requireDelete(ctx, args, "/v1/ip-rules/", t)
	case "list_api_keys":
		return e.get(ctx, "/v1/api-keys", t)
	case "create_api_key":
		return e.createAPIKey(ctx, args, t)
	case "revoke_api_key":
		return e.requireDelete(ctx, args, "/v1/api-keys/", t)

	// --- infrastructure (via bridge) ---
	case "get_status":
		return e.getStatus(ctx, t)
	case "list_modules":
		return e.listModules(ctx, t)
	case "install_module":
		return e.installModule(ctx, args, t)
	case "trigger_update":
		return e.triggerUpdate(ctx, t)

	default:
		return argErr("unknown action: "+action, t)
	}
}

// ============================================================
// Settings actions
// ============================================================

func (e *Executor) setSetting(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	key := str(a, "key")
	if key == "" {
		return argErr("key is required", t)
	}
	val, ok := a["value"].(string)
	if !ok {
		return argErr("value is required", t)
	}
	return e.put(ctx, "/v1/admin/platform-settings/"+key, map[string]any{"value": val}, t)
}

func (e *Executor) configureEmail(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "from") == "" || str(a, "smtp_host") == "" {
		return argErr("from and smtp_host are required", t)
	}
	body := map[string]any{"from": a["from"], "smtp_host": a["smtp_host"]}
	for _, k := range []string{"smtp_port", "smtp_user", "smtp_pass", "smtp_tls_mode"} {
		if v := str(a, k); v != "" {
			body[k] = v
		}
	}
	return e.put(ctx, "/v1/admin/email/config", body, t)
}

func (e *Executor) testEmail(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "to") == "" {
		return argErr("to is required", t)
	}
	return e.post(ctx, "/v1/admin/email/test", map[string]any{"to": a["to"]}, t)
}

func (e *Executor) configureSMTP(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	for _, f := range []string{"name", "from_addr", "smtp_host", "smtp_pass", "tls_mode"} {
		if str(a, f) == "" {
			return argErr(f+" is required", t)
		}
	}
	port, ok := intVal(a["smtp_port"])
	if !ok || port < 0 || port > 65535 {
		return argErr("smtp_port must be 0-65535", t)
	}
	return e.post(ctx, "/v1/admin/smtp-providers", map[string]any{
		"name": a["name"], "from_addr": a["from_addr"],
		"smtp_host": a["smtp_host"], "smtp_port": port,
		"smtp_user": a["smtp_user"], "smtp_pass": a["smtp_pass"],
		"tls_mode": a["tls_mode"],
	}, t)
}

func (e *Executor) configureCaptcha(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "site_key") == "" || str(a, "secret_key") == "" {
		return argErr("site_key and secret_key are required", t)
	}
	res := e.put(ctx, "/v1/admin/platform-settings/turnstile.site_key", map[string]any{"value": a["site_key"]}, t)
	if res.Error != nil {
		return res
	}
	return e.put(ctx, "/v1/admin/platform-settings/turnstile.secret_key", map[string]any{"value": a["secret_key"]}, t)
}

func (e *Executor) configureRegistration(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	mode := str(a, "mode")
	if mode != "open" && mode != "invite_only" {
		return argErr("mode must be 'open' or 'invite_only'", t)
	}
	val := "true"
	if mode == "invite_only" {
		val = "false"
	}
	return e.put(ctx, "/v1/admin/platform-settings/registration.open", map[string]any{"value": val}, t)
}

func (e *Executor) configureGateway(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	body := make(map[string]any)
	if v := str(a, "ip_mode"); v != "" {
		body["ip_mode"] = v
	}
	if v, ok := a["trusted_cidrs"]; ok {
		body["trusted_cidrs"] = v
	}
	if v, ok := intVal(a["risk_reject_threshold"]); ok {
		body["risk_reject_threshold"] = v
	}
	if v, ok := floatVal(a["rate_limit_capacity"]); ok {
		body["rate_limit_capacity"] = v
	}
	if v, ok := floatVal(a["rate_limit_per_minute"]); ok {
		body["rate_limit_per_minute"] = v
	}
	if len(body) == 0 {
		return argErr("at least one parameter is required", t)
	}
	return e.put(ctx, "/v1/admin/gateway-config", body, t)
}

func (e *Executor) updateStyles(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "css") == "" {
		return argErr("css is required", t)
	}
	return e.put(ctx, "/v1/admin/platform-settings/custom_css", map[string]any{"value": a["css"]}, t)
}

// ============================================================
// Provider actions
// ============================================================

func (e *Executor) addProvider(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "name") == "" || str(a, "provider") == "" || str(a, "api_key") == "" {
		return argErr("name, provider, and api_key are required", t)
	}
	body := map[string]any{"name": a["name"], "provider": a["provider"], "api_key": a["api_key"]}
	if v := str(a, "base_url"); v != "" {
		body["base_url"] = v
	}
	return e.post(ctx, "/v1/llm-providers", body, t)
}

func (e *Executor) updateProvider(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	id := str(a, "id")
	if id == "" {
		return argErr("id is required", t)
	}
	body := make(map[string]any)
	for _, k := range []string{"name", "api_key", "base_url"} {
		if v := str(a, k); v != "" {
			body[k] = v
		}
	}
	return e.patch(ctx, "/v1/llm-providers/"+id, body, t)
}

func (e *Executor) listModels(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "provider_id") == "" {
		return argErr("provider_id is required", t)
	}
	return e.get(ctx, "/v1/llm-providers/"+str(a, "provider_id")+"/models", t)
}

func (e *Executor) configureModel(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	pid, mid := str(a, "provider_id"), str(a, "model_id")
	if pid == "" || mid == "" {
		return argErr("provider_id and model_id are required", t)
	}
	body := make(map[string]any)
	if cfg, ok := a["config"]; ok && cfg != nil {
		body["config"] = cfg
	}
	return e.patch(ctx, "/v1/llm-providers/"+pid+"/models/"+mid, body, t)
}

// ============================================================
// Agent (persona) actions
// ============================================================

func (e *Executor) createAgent(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "persona_key") == "" || str(a, "display_name") == "" || str(a, "prompt_md") == "" {
		return argErr("persona_key, display_name, and prompt_md are required", t)
	}
	body := make(map[string]any, len(a))
	for k, v := range a {
		if k == "action" {
			continue
		}
		body[k] = v
	}
	return e.post(ctx, "/v1/personas", body, t)
}

func (e *Executor) updateAgent(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	id := str(a, "id")
	if id == "" {
		return argErr("id is required", t)
	}
	body := make(map[string]any)
	for k, v := range a {
		if k == "action" || k == "id" {
			continue
		}
		body[k] = v
	}
	return e.patch(ctx, "/v1/personas/"+id, body, t)
}

// ============================================================
// Skill actions
// ============================================================

func (e *Executor) installSkillMarket(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "skill_id") == "" {
		return argErr("skill_id is required", t)
	}
	return e.post(ctx, "/v1/skill-packages/import/registry", map[string]any{"skill_id": a["skill_id"]}, t)
}

func (e *Executor) installSkillGithub(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "url") == "" {
		return argErr("url is required", t)
	}
	body := map[string]any{"url": a["url"]}
	if v := str(a, "ref"); v != "" {
		body["ref"] = v
	}
	return e.post(ctx, "/v1/skill-packages/import/github", body, t)
}

// ============================================================
// MCP actions
// ============================================================

func (e *Executor) addMCPConfig(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "name") == "" {
		return argErr("name is required", t)
	}
	transport := str(a, "transport")
	if transport != "stdio" && transport != "http_sse" && transport != "streamable_http" {
		return argErr("transport must be stdio, http_sse, or streamable_http", t)
	}
	if transport == "stdio" && str(a, "command") == "" {
		return argErr("command is required for stdio transport", t)
	}
	if transport != "stdio" && str(a, "url") == "" {
		return argErr("url is required for "+transport+" transport", t)
	}

	body := map[string]any{"name": a["name"], "transport": transport}
	for _, k := range []string{"url", "command", "bearer_token", "cwd"} {
		if v := str(a, k); v != "" {
			body[k] = v
		}
	}
	for _, k := range []string{"args", "env", "inherit_parent_env"} {
		if v, ok := a[k]; ok {
			body[k] = v
		}
	}
	if v, ok := intVal(a["call_timeout_ms"]); ok {
		body["call_timeout_ms"] = v
	}
	return e.post(ctx, "/v1/mcp-configs", body, t)
}

func (e *Executor) updateMCPConfig(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	id := str(a, "id")
	if id == "" {
		return argErr("id is required", t)
	}
	body := make(map[string]any)
	for _, k := range []string{"name", "url", "bearer_token"} {
		if v := str(a, k); v != "" {
			body[k] = v
		}
	}
	if v, ok := intVal(a["call_timeout_ms"]); ok {
		body["call_timeout_ms"] = v
	}
	if v, ok := a["is_active"]; ok {
		body["is_active"] = v
	}
	return e.patch(ctx, "/v1/mcp-configs/"+id, body, t)
}

func (e *Executor) addToolProvider(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	g, p := str(a, "group"), str(a, "provider")
	if g == "" || p == "" {
		return argErr("group and provider are required", t)
	}
	body := make(map[string]any)
	if v := str(a, "api_key"); v != "" {
		body["api_key"] = v
	}
	if v := str(a, "base_url"); v != "" {
		body["base_url"] = v
	}
	return e.put(ctx, "/v1/tool-providers/"+g+"/"+p+"/activate", body, t)
}

func (e *Executor) updateToolProvider(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	g, p := str(a, "group"), str(a, "provider")
	if g == "" || p == "" {
		return argErr("group and provider are required", t)
	}
	body := make(map[string]any)
	if cfg, ok := a["config"].(map[string]any); ok {
		for k, v := range cfg {
			body[k] = v
		}
	}
	return e.patch(ctx, "/v1/tool-providers/"+g+"/"+p+"/config", body, t)
}

// ============================================================
// Access actions
// ============================================================

func (e *Executor) addIPRule(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	ruleType := str(a, "type")
	if ruleType != "allowlist" && ruleType != "blocklist" {
		return argErr("type must be 'allowlist' or 'blocklist'", t)
	}
	if str(a, "cidr") == "" {
		return argErr("cidr is required", t)
	}
	body := map[string]any{"type": ruleType, "cidr": a["cidr"]}
	if v := str(a, "note"); v != "" {
		body["note"] = v
	}
	return e.post(ctx, "/v1/ip-rules", body, t)
}

func (e *Executor) createAPIKey(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if str(a, "name") == "" {
		return argErr("name is required", t)
	}
	body := map[string]any{"name": a["name"]}
	if arr, ok := a["scopes"].([]any); ok {
		body["scopes"] = arr
	}
	return e.post(ctx, "/v1/api-keys", body, t)
}

// ============================================================
// Infrastructure actions (via bridge)
// ============================================================

func (e *Executor) getStatus(ctx context.Context, t time.Time) tools.ExecutionResult {
	if e.bridgeBase == "" {
		return degraded("bridge not configured", t)
	}
	result := map[string]any{}
	if detect, err := e.bridgeGet(ctx, "/v1/platform/detect"); err == nil {
		result["platform"] = detect
	} else {
		return degraded("platform detection unavailable", t)
	}
	if modules, err := e.bridgeGet(ctx, "/v1/modules"); err == nil {
		result["modules"] = modules
	}
	if version, err := e.bridgeGet(ctx, "/v1/system/version"); err == nil {
		result["version"] = version
	}
	return tools.ExecutionResult{ResultJSON: result, DurationMs: ms(t)}
}

func (e *Executor) listModules(ctx context.Context, t time.Time) tools.ExecutionResult {
	if e.bridgeBase == "" {
		return degraded("bridge not configured", t)
	}
	raw, err := e.bridgeGet(ctx, "/v1/modules")
	if err != nil {
		return degraded("module listing unavailable", t)
	}
	return tools.ExecutionResult{ResultJSON: map[string]any{"modules": raw}, DurationMs: ms(t)}
}

func (e *Executor) installModule(ctx context.Context, a map[string]any, t time.Time) tools.ExecutionResult {
	if e.bridgeBase == "" {
		return degraded("bridge not configured", t)
	}
	name := str(a, "name")
	if name == "" {
		return argErr("name is required", t)
	}
	data, _ := json.Marshal(map[string]any{"action": "install"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.bridgeBase+"/v1/modules/"+name+"/actions", bytes.NewReader(data))
	if err != nil {
		return apiErr("build request", err, t)
	}
	req.Header.Set("Content-Type", "application/json")
	return e.doBridge(req, t)
}

func (e *Executor) triggerUpdate(ctx context.Context, t time.Time) tools.ExecutionResult {
	if e.bridgeBase == "" {
		return degraded("bridge not configured", t)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.bridgeBase+"/v1/system/upgrade", nil)
	if err != nil {
		return apiErr("build request", err, t)
	}
	return e.doBridge(req, t)
}

// ============================================================
// Shared: requireGet / requireDelete (id-based convenience)
// ============================================================

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (e *Executor) requireGet(ctx context.Context, a map[string]any, pathPrefix string, t time.Time) tools.ExecutionResult {
	id := str(a, "id")
	if id == "" {
		return argErr("id is required", t)
	}
	if !uuidRe.MatchString(id) {
		return argErr("id must be a valid UUID", t)
	}
	return e.get(ctx, pathPrefix+id, t)
}

func (e *Executor) requireDelete(ctx context.Context, a map[string]any, pathPrefix string, t time.Time) tools.ExecutionResult {
	id := str(a, "id")
	if id == "" {
		return argErr("id is required", t)
	}
	if !uuidRe.MatchString(id) {
		return argErr("id must be a valid UUID", t)
	}
	return e.del(ctx, pathPrefix+id, t)
}

// ============================================================
// HTTP helpers (API service)
// ============================================================

func (e *Executor) get(ctx context.Context, path string, t time.Time) tools.ExecutionResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.apiBase+path, nil)
	if err != nil {
		return apiErr("build request", err, t)
	}
	return e.doAPI(req, t)
}

func (e *Executor) post(ctx context.Context, path string, body map[string]any, t time.Time) tools.ExecutionResult {
	return e.bodyReq(ctx, http.MethodPost, path, body, t)
}

func (e *Executor) put(ctx context.Context, path string, body map[string]any, t time.Time) tools.ExecutionResult {
	return e.bodyReq(ctx, http.MethodPut, path, body, t)
}

func (e *Executor) patch(ctx context.Context, path string, body map[string]any, t time.Time) tools.ExecutionResult {
	return e.bodyReq(ctx, http.MethodPatch, path, body, t)
}

func (e *Executor) del(ctx context.Context, path string, t time.Time) tools.ExecutionResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, e.apiBase+path, nil)
	if err != nil {
		return apiErr("build request", err, t)
	}
	return e.doAPI(req, t)
}

func (e *Executor) bodyReq(ctx context.Context, method, path string, body map[string]any, t time.Time) tools.ExecutionResult {
	data, err := json.Marshal(body)
	if err != nil {
		return apiErr("marshal body", err, t)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.apiBase+path, bytes.NewReader(data))
	if err != nil {
		return apiErr("build request", err, t)
	}
	req.Header.Set("Content-Type", "application/json")
	return e.doAPI(req, t)
}

func (e *Executor) doAPI(req *http.Request, t time.Time) tools.ExecutionResult {
	req.Header.Set("Authorization", "Bearer "+e.tp.Token())
	resp, err := e.http.Do(req)
	if err != nil {
		return apiErr(req.Method+" "+req.URL.Path, err, t)
	}
	defer resp.Body.Close()
	return parseResponse(resp, t)
}

// ============================================================
// HTTP helpers (bridge)
// ============================================================

func (e *Executor) bridgeGet(ctx context.Context, path string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.bridgeBase+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("bridge %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		var arr []any
		if err2 := json.Unmarshal(body, &arr); err2 == nil {
			return arr, nil
		}
		return nil, err
	}
	return obj, nil
}

func (e *Executor) doBridge(req *http.Request, t time.Time) tools.ExecutionResult {
	resp, err := e.http.Do(req)
	if err != nil {
		return degraded("bridge unreachable", t)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errHTTP,
				Message:    fmt.Sprintf("bridge returned %d: %s", resp.StatusCode, string(body)),
			},
			DurationMs: ms(t),
		}
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		result = map[string]any{"status": "accepted"}
	}
	return tools.ExecutionResult{ResultJSON: result, DurationMs: ms(t)}
}

// ============================================================
// Shared response parsing & utilities
// ============================================================

func parseResponse(resp *http.Response, t time.Time) tools.ExecutionResult {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errUnauthorized, Message: fmt.Sprintf("API returned %d", resp.StatusCode)},
			DurationMs: ms(t),
		}
	}
	if resp.StatusCode == http.StatusNoContent {
		return tools.ExecutionResult{ResultJSON: map[string]any{"status": "ok"}, DurationMs: ms(t)}
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errHTTP, Message: fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))},
			DurationMs: ms(t),
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return apiErr("read response", err, t)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		var arr []any
		if err2 := json.Unmarshal(body, &arr); err2 == nil {
			return tools.ExecutionResult{ResultJSON: map[string]any{"items": arr}, DurationMs: ms(t)}
		}
		return apiErr("decode response", err, t)
	}
	return tools.ExecutionResult{ResultJSON: result, DurationMs: ms(t)}
}

func str(a map[string]any, key string) string {
	v, _ := a[key].(string)
	return strings.TrimSpace(v)
}

func intVal(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func floatVal(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func ms(t time.Time) int {
	d := int(time.Since(t) / time.Millisecond)
	if d < 0 {
		return 0
	}
	return d
}

func argErr(msg string, t time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errArgsInvalid, Message: msg},
		DurationMs: ms(t),
	}
}

func apiErr(op string, err error, t time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errAPICall, Message: op + ": " + err.Error()},
		DurationMs: ms(t),
	}
}

func degraded(reason string, t time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "degraded", "message": reason},
		DurationMs: ms(t),
	}
}
