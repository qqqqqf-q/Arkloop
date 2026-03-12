package auth

import (
	"slices"
	"testing"
)

func TestPermissionsForRole(t *testing.T) {
	cases := []struct {
		role     string
		wantNil  bool
		mustHave []string
		mustNot  []string
	}{
		{
			role:     RolePlatformAdmin,
			mustHave: []string{PermPlatformAdmin, PermOrgMembersInvite, PermDataThreadsRead},
		},
		{
			role: RoleAccountAdmin,
			mustHave: []string{
				PermOrgMembersInvite, PermOrgMembersList, PermOrgMembersRevoke,
				PermOrgTeamsRead, PermOrgTeamsManage,
				PermDataThreadsRead,
				PermDataProjectsRead, PermDataProjectsManage,
			},
			mustNot: []string{PermPlatformAdmin},
		},
		{
			// 旧值 "owner" 兼容
			role: "owner",
			mustHave: []string{
				PermOrgMembersInvite, PermDataThreadsRead,
				PermOrgTeamsManage, PermDataProjectsManage,
			},
			mustNot: []string{PermPlatformAdmin},
		},
		{
			role: RoleAccountMember,
			mustHave: []string{
				PermDataThreadsRead, PermDataRunsRead, PermDataAPIKeysManage,
				PermOrgTeamsRead, PermDataProjectsRead,
			},
			mustNot: []string{
				PermPlatformAdmin, PermOrgMembersInvite,
				PermOrgTeamsManage, PermDataProjectsManage,
			},
		},
		{
			// 旧值 "member" 兼容
			role: "member",
			mustHave: []string{PermDataThreadsRead, PermOrgTeamsRead},
			mustNot:  []string{PermOrgMembersInvite, PermOrgTeamsManage, PermDataProjectsManage},
		},
		{
			role:    "unknown",
			wantNil: true,
		},
		{
			role:    "",
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			perms := PermissionsForRole(tc.role)
			if tc.wantNil {
				if perms != nil {
					t.Fatalf("expected nil, got %v", perms)
				}
				return
			}
			if perms == nil {
				t.Fatalf("unexpected nil permissions for role %q", tc.role)
			}
			for _, p := range tc.mustHave {
				if !slices.Contains(perms, p) {
					t.Errorf("role %q missing permission %q", tc.role, p)
				}
			}
			for _, p := range tc.mustNot {
				if slices.Contains(perms, p) {
					t.Errorf("role %q should not have permission %q", tc.role, p)
				}
			}
		})
	}
}

func TestPermissionsForRoleAccountAdminEqualsOwner(t *testing.T) {
	a := PermissionsForRole(RoleAccountAdmin)
	b := PermissionsForRole("owner")
	if len(a) != len(b) {
		t.Fatalf("account_admin(%d) and owner(%d) perm counts differ", len(a), len(b))
	}
	for _, p := range a {
		if !slices.Contains(b, p) {
			t.Errorf("owner missing %q", p)
		}
	}
}

func TestPermissionsForRoleAccountMemberEqualsMember(t *testing.T) {
	a := PermissionsForRole(RoleAccountMember)
	b := PermissionsForRole("member")
	if len(a) != len(b) {
		t.Fatalf("account_member(%d) and member(%d) perm counts differ", len(a), len(b))
	}
	for _, p := range a {
		if !slices.Contains(b, p) {
			t.Errorf("member missing %q", p)
		}
	}
}
