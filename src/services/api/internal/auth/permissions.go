package auth

import (
	"sort"
	"strings"
)

// 系统权限点常量。
const (
	PermPlatformAdmin         = "platform.admin"
	PermAccountMembersInvite  = "org.members.invite"
	PermAccountMembersList   = "org.members.list"
	PermAccountMembersRevoke  = "org.members.revoke"
	PermAccountTeamsRead      = "org.teams.read"
	PermAccountTeamsManage    = "org.teams.manage"
	PermDataThreadsRead       = "data.threads.read"
	PermDataThreadsWrite      = "data.threads.write"
	PermDataRunsRead          = "data.runs.read"
	PermDataRunsWrite         = "data.runs.write"
	PermDataAPIKeysManage     = "data.api_keys.manage"
	PermDataPersonasRead      = "data.personas.read"
	PermDataPersonasManage    = "data.personas.manage"
	PermDataLLMCreds          = "data.llm_credentials.manage"
	PermDataMCPConfigs        = "data.mcp_configs.manage"
	PermDataSecrets           = "data.secrets.manage"
	PermDataProjectsRead      = "data.projects.read"
	PermDataProjectsManage    = "data.projects.manage"
	PermDataSkillsRead        = "data.skills.read"
	PermDataSkillsManage      = "data.skills.manage"
	PermDataWebhooksManage    = "data.webhooks.manage"
	PermDataSubscriptionsRead = "data.subscriptions.read"
	PermDataUsageRead         = "data.usage.read"
	PermAccountAuditRead      = "org.audit_read"

	PermPlatformPlansManage         = "platform.plans.manage"
	PermPlatformSubscriptionsManage = "platform.subscriptions.manage"
	PermPlatformEntitlementsManage  = "platform.entitlements.manage"
	PermPlatformFeatureFlagsManage  = "platform.feature_flags.manage"
	PermPlatformInviteCodesManage   = "platform.invite_codes.manage"
)

var allPermissions = []string{
	PermPlatformAdmin,
	PermAccountMembersInvite,
	PermAccountMembersList,
	PermAccountMembersRevoke,
	PermAccountTeamsRead,
	PermAccountTeamsManage,
	PermDataThreadsRead,
	PermDataThreadsWrite,
	PermDataRunsRead,
	PermDataRunsWrite,
	PermDataAPIKeysManage,
	PermDataPersonasRead,
	PermDataPersonasManage,
	PermDataLLMCreds,
	PermDataMCPConfigs,
	PermDataSecrets,
	PermDataProjectsRead,
	PermDataProjectsManage,
	PermDataSkillsRead,
	PermDataSkillsManage,
	PermDataWebhooksManage,
	PermDataSubscriptionsRead,
	PermDataUsageRead,
	PermAccountAuditRead,
	PermPlatformPlansManage,
	PermPlatformSubscriptionsManage,
	PermPlatformEntitlementsManage,
	PermPlatformFeatureFlagsManage,
	PermPlatformInviteCodesManage,
}

func KnownPermissions() []string {
	out := make([]string, len(allPermissions))
	copy(out, allPermissions)
	return out
}

func NormalizePermissions(values []string) ([]string, []string) {
	seen := make(map[string]struct{}, len(values))
	known := permissionSet(allPermissions)
	normalized := make([]string, 0, len(values))
	invalid := make([]string, 0)
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		if _, ok := known[cleaned]; !ok {
			invalid = append(invalid, cleaned)
			continue
		}
		normalized = append(normalized, cleaned)
	}
	sort.Strings(normalized)
	sort.Strings(invalid)
	return normalized, invalid
}

func IntersectPermissions(left []string, right []string) []string {
	leftSet := permissionSet(left)
	intersection := make([]string, 0, len(right))
	for _, value := range right {
		if _, ok := leftSet[value]; ok {
			intersection = append(intersection, value)
		}
	}
	sort.Strings(intersection)
	return dedupeSortedStrings(intersection)
}

func IsPermissionSubset(scopes []string, allowed []string) bool {
	allowedSet := permissionSet(allowed)
	for _, scope := range scopes {
		if _, ok := allowedSet[scope]; !ok {
			return false
		}
	}
	return true
}

func permissionSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		set[cleaned] = struct{}{}
	}
	return set
}

func dedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value == out[len(out)-1] {
			continue
		}
		out = append(out, value)
	}
	return out
}
