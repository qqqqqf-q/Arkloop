package deorg

import (
	"fmt"
	"sort"
	"strings"
)

type LegacyShapeSummary struct {
	DistinctOwnerCount           int
	TeamCount                    int
	TeamMembershipCount          int
	MultiMemberOrgIDs            []string
	MissingOwnerOrgIDs           []string
	MissingDefaultProjectOrgIDs  []string
}

func ValidateLegacyShape(summary LegacyShapeSummary) error {
	if summary.TeamCount > 0 || summary.TeamMembershipCount > 0 {
		return fmt.Errorf("deorg export only supports single-owner data without teams")
	}
	if len(summary.MultiMemberOrgIDs) > 0 {
		return fmt.Errorf("deorg export found multi-member orgs: %s", joinSorted(summary.MultiMemberOrgIDs))
	}
	if len(summary.MissingOwnerOrgIDs) > 0 {
		return fmt.Errorf("deorg export found orgs without owner_user_id: %s", joinSorted(summary.MissingOwnerOrgIDs))
	}
	if len(summary.MissingDefaultProjectOrgIDs) > 0 {
		return fmt.Errorf("deorg export found orgs without default project: %s", joinSorted(summary.MissingDefaultProjectOrgIDs))
	}
	if summary.DistinctOwnerCount > 1 {
		return fmt.Errorf("deorg export only supports a single owner user")
	}
	return nil
}

func ValidateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.Version) != ManifestVersion {
		return fmt.Errorf("manifest version must be %s", ManifestVersion)
	}
	if len(manifest.LegacyOrgMappings) == 0 && len(manifest.LegacyOrgs) > 0 {
		return fmt.Errorf("manifest legacy_org_mappings must not be empty")
	}
	projectIDs := make(map[string]struct{}, len(manifest.Projects))
	for _, item := range manifest.Projects {
		projectIDs[item.ID] = struct{}{}
	}
	userIDs := make(map[string]struct{}, len(manifest.Users))
	for _, item := range manifest.Users {
		userIDs[item.ID] = struct{}{}
	}
	for _, mapping := range manifest.LegacyOrgMappings {
		if _, ok := projectIDs[mapping.DefaultProjectID]; !ok {
			return fmt.Errorf("manifest default project not found for org %s", mapping.OrgID)
		}
		if _, ok := userIDs[mapping.OwnerUserID]; !ok {
			return fmt.Errorf("manifest owner user not found for org %s", mapping.OrgID)
		}
	}
	return nil
}

func joinSorted(values []string) string {
	items := append([]string(nil), values...)
	sort.Strings(items)
	return strings.Join(items, ", ")
}
