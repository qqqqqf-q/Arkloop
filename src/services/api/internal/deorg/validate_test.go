package deorg

import "testing"

func TestValidateLegacyShapeRejectsMultiMemberOrg(t *testing.T) {
	err := ValidateLegacyShape(LegacyShapeSummary{MultiMemberOrgIDs: []string{"org-b", "org-a"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "deorg export found multi-member orgs: org-a, org-b" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestValidateLegacyShapeRejectsTeams(t *testing.T) {
	err := ValidateLegacyShape(LegacyShapeSummary{TeamCount: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateManifestRequiresMappings(t *testing.T) {
	err := ValidateManifest(Manifest{Version: ManifestVersion, LegacyOrgs: []LegacyOrg{{ID: "org-1"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateManifestRequiresMappedProjectAndUser(t *testing.T) {
	err := ValidateManifest(Manifest{
		Version: ManifestVersion,
		LegacyOrgMappings: []LegacyOrgMapping{{OrgID: "org-1", OwnerUserID: "user-1", DefaultProjectID: "proj-1"}},
		Users: []UserRecord{{ID: "user-1"}},
		Projects: []ProjectRecord{{ID: "proj-1"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
