package auth

// 系统内置角色名。
const (
	RolePlatformAdmin = "platform_admin"
	RoleOrgAdmin      = "org_admin"
	RoleOrgMember     = "org_member"
)

var orgAdminPerms = []string{
	PermOrgMembersInvite, PermOrgMembersList, PermOrgMembersRevoke,
	PermOrgTeamsRead, PermOrgTeamsManage,
	PermDataThreadsRead, PermDataThreadsWrite,
	PermDataRunsRead, PermDataRunsWrite,
	PermDataAPIKeysManage,
	PermDataSkillsRead,
	PermDataLLMCreds,
	PermDataMCPConfigs,
	PermDataSecrets,
	PermDataProjectsRead, PermDataProjectsManage,
	PermDataAgentConfigsRead, PermDataAgentConfigsManage,
	PermDataSubscriptionsRead,
	PermDataUsageRead,
	PermOrgAuditRead,
}

var orgMemberPerms = []string{
	PermOrgTeamsRead,
	PermDataThreadsRead, PermDataThreadsWrite,
	PermDataRunsRead, PermDataRunsWrite,
	// 成员可管理自己名下的 API Key，符合最小特权原则——Key 归属于创建者，不涉及 org 级写操作。
	PermDataAPIKeysManage,
	PermDataSkillsRead,
	PermDataProjectsRead,
	PermDataAgentConfigsRead,
	PermDataSubscriptionsRead,
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
	orgAdminPerms...,
)

// PermissionsForRole 根据角色名返回权限集合的副本，同时兼容旧值 "owner"/"member"。
// 返回副本防止调用方修改底层全局切片。未知角色返回 nil。
func PermissionsForRole(role string) []string {
	var src []string
	switch role {
	case RolePlatformAdmin:
		src = platformAdminPerms
	case RoleOrgAdmin, "owner":
		src = orgAdminPerms
	case RoleOrgMember, "member":
		src = orgMemberPerms
	default:
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
