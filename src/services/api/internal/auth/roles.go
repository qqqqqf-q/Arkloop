package auth

// 系统内置角色名。
const (
	RolePlatformAdmin = "platform_admin"
	RoleAccountAdmin  = "account_admin"
	RoleAccountMember = "account_member"
	RoleSystemAgent   = "system_agent"
)

var accountAdminPerms = []string{
	PermAccountMembersInvite, PermAccountMembersList, PermAccountMembersRevoke,
	PermAccountTeamsRead, PermAccountTeamsManage,
	PermDataThreadsRead, PermDataThreadsWrite,
	PermDataRunsRead, PermDataRunsWrite,
	PermDataAPIKeysManage,
	PermDataPersonasRead, PermDataPersonasManage,
	PermDataLLMCreds,
	PermDataMCPConfigs,
	PermDataSecrets,
	PermDataProjectsRead, PermDataProjectsManage,
	PermDataSkillsRead, PermDataSkillsManage,
	PermDataSubscriptionsRead,
	PermDataUsageRead,
	PermAccountAuditRead,
}

var accountMemberPerms = []string{
	PermAccountTeamsRead,
	PermDataThreadsRead, PermDataThreadsWrite,
	PermDataRunsRead, PermDataRunsWrite,
	// 成员可管理自己名下的 API Key，符合最小特权原则，不涉及 account 级写操作。
	PermDataAPIKeysManage,
	PermDataPersonasRead,
	PermDataProjectsRead,
	PermDataSkillsRead, PermDataSkillsManage,
	PermDataSubscriptionsRead,
}

var systemAgentPerms = []string{
	PermDataPersonasRead, PermDataPersonasManage,
	PermDataSkillsRead, PermDataSkillsManage,
	PermDataLLMCreds,
	PermDataMCPConfigs,
	PermDataProjectsRead, PermDataProjectsManage,
	PermDataWebhooksManage,
	PermPlatformFeatureFlagsManage,
}

var platformAdminPerms = append(
	[]string{
		PermPlatformAdmin,
		PermPlatformPlansManage,
		PermPlatformSubscriptionsManage,
		PermPlatformEntitlementsManage,
		PermPlatformFeatureFlagsManage,
		PermPlatformInviteCodesManage,
	},
	accountAdminPerms...,
)

// PermissionsForRole 根据角色名返回权限集合的副本，同时兼容旧值 "owner"/"member"。
// 返回副本防止调用方修改底层全局切片。未知角色返回 nil。
func PermissionsForRole(role string) []string {
	var src []string
	switch role {
	case RolePlatformAdmin:
		src = platformAdminPerms
	case RoleAccountAdmin, "owner":
		src = accountAdminPerms
	case RoleAccountMember, "member":
		src = accountMemberPerms
	case RoleSystemAgent:
		src = systemAgentPerms
	default:
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
