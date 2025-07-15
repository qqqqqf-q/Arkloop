package catalogapi

import (
	"encoding/json"
	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/skillstore"
)

type externalSkillDirEntry struct {
	Path   string                    `json:"path"`
	Skills []skillstore.ExternalSkill `json:"skills"`
}

func externalSkillsDiscoverEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	settingsRepo *data.PlatformSettingsRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		traceID := observability.TraceIDFromContext(r.Context())
		if _, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil); !ok {
			return
		}

		var configuredDirs []string
		if settingsRepo != nil {
			setting, err := settingsRepo.Get(r.Context(), "skills.external_dirs")
			if err == nil && setting != nil {
				_ = json.Unmarshal([]byte(setting.Value), &configuredDirs)
			}
		}
		configuredDirs = append(configuredDirs, skillstore.WellKnownSkillDirs()...)

		seen := make(map[string]struct{})
		var dirs []string
		for _, d := range configuredDirs {
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				dirs = append(dirs, d)
			}
		}

		result := make([]externalSkillDirEntry, 0, len(dirs))
		for _, dir := range dirs {
			skills := skillstore.DiscoverExternalSkills([]string{dir})
			result = append(result, externalSkillDirEntry{
				Path:   dir,
				Skills: skills,
			})
		}

		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"dirs": result})
	}
}
