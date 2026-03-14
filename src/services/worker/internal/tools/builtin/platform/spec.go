package platform

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const toolName = "platform_manage"

func sp(s string) *string { return &s }

var actions = []string{
	"get_settings", "set_setting",
	"configure_email", "test_email", "configure_smtp",
	"configure_captcha", "configure_registration", "configure_gateway", "update_styles",
	"list_providers", "add_provider", "update_provider", "delete_provider",
	"list_models", "configure_model",
	"list_agents", "create_agent", "update_agent", "delete_agent", "get_agent",
	"list_skills", "install_skill_market", "install_skill_github", "remove_skill",
	"list_mcp_configs", "add_mcp_config", "update_mcp_config", "delete_mcp_config",
	"list_tool_providers", "add_tool_provider", "update_tool_provider",
	"list_ip_rules", "add_ip_rule", "delete_ip_rule",
	"list_api_keys", "create_api_key", "revoke_api_key",
	"get_status", "list_modules", "install_module", "trigger_update",
}

var AgentSpec = tools.AgentToolSpec{
	Name:        toolName,
	Version:     "1",
	Description: "Unified platform management tool",
	SideEffects: true,
	RiskLevel:   tools.RiskLevelHigh,
}

var LlmSpec = llm.ToolSpec{
	Name: toolName,
	Description: sp(
		"Platform management tool. Pass action and params object.\n" +
			"Settings: get_settings | set_setting{key,value} | configure_email{from,smtp_host} | test_email{to} | configure_smtp{name,from_addr,smtp_host,smtp_port,smtp_pass,tls_mode} | configure_captcha{site_key,secret_key} | configure_registration{mode} | configure_gateway{ip_mode,...} | update_styles{css}\n" +
			"Providers: list_providers | add_provider{name,provider,api_key} | update_provider{id,...} | delete_provider{id} | list_models{provider_id} | configure_model{provider_id,model_id,config?}\n" +
			"Agents: list_agents | create_agent{persona_key,display_name,prompt_md} | update_agent{id,...} | delete_agent{id} | get_agent{id}\n" +
			"Skills: list_skills | install_skill_market{skill_id} | install_skill_github{url,ref?} | remove_skill{id}\n" +
			"MCP: list_mcp_configs | add_mcp_config{name,transport,command?,url?} | update_mcp_config{id,...} | delete_mcp_config{id} | list_tool_providers | add_tool_provider{group,provider} | update_tool_provider{group,provider,config?}\n" +
			"Access: list_ip_rules | add_ip_rule{type,cidr} | delete_ip_rule{id} | list_api_keys | create_api_key{name} | revoke_api_key{id}\n" +
			"Infra: get_status | list_modules | install_module{name} | trigger_update",
	),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": actions, "description": "The operation to perform"},
			"params": map[string]any{"type": "object", "description": "Action parameters (key-value pairs specific to the action)"},
		},
		"required": []string{"action"},
	},
}

func AgentSpecs() []tools.AgentToolSpec { return []tools.AgentToolSpec{AgentSpec} }
func LlmSpecs() []llm.ToolSpec          { return []llm.ToolSpec{LlmSpec} }
