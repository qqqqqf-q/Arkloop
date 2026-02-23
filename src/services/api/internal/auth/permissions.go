package auth

// 系统权限点常量。
const (
	PermPlatformAdmin          = "platform.admin"
	PermOrgMembersInvite       = "org.members.invite"
	PermOrgMembersList         = "org.members.list"
	PermOrgMembersRevoke       = "org.members.revoke"
	PermOrgTeamsRead           = "org.teams.read"
	PermOrgTeamsManage         = "org.teams.manage"
	PermDataThreadsRead        = "data.threads.read"
	PermDataThreadsWrite       = "data.threads.write"
	PermDataRunsRead           = "data.runs.read"
	PermDataRunsWrite          = "data.runs.write"
	PermDataAPIKeysManage      = "data.api_keys.manage"
	PermDataSkillsRead         = "data.skills.read"
	PermDataLLMCreds           = "data.llm_credentials.manage"
	PermDataMCPConfigs         = "data.mcp_configs.manage"
	PermDataSecrets            = "data.secrets.manage"
	PermDataProjectsRead       = "data.projects.read"
	PermDataProjectsManage     = "data.projects.manage"
	PermDataWebhooksManage     = "data.webhooks.manage"
	PermDataAgentConfigsRead   = "data.agent_configs.read"
	PermDataAgentConfigsManage = "data.agent_configs.manage"
	PermDataSubscriptionsRead  = "data.subscriptions.read"

	PermPlatformPlansManage         = "platform.plans.manage"
	PermPlatformSubscriptionsManage = "platform.subscriptions.manage"
	PermPlatformEntitlementsManage  = "platform.entitlements.manage"
)
