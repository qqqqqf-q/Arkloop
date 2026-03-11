package deorg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Export(ctx context.Context, pool *pgxpool.Pool) (Manifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return Manifest{}, fmt.Errorf("pool must not be nil")
	}

	summary, err := loadLegacyShapeSummary(ctx, pool)
	if err != nil {
		return Manifest{}, err
	}
	if err := ValidateLegacyShape(summary); err != nil {
		return Manifest{}, err
	}

	mappings, err := loadLegacyOrgMappings(ctx, pool)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		Version:           ManifestVersion,
		ExportedAt:        time.Now().UTC(),
		LegacyOrgMappings: mappings,
	}

	if manifest.LegacyOrgs, err = loadLegacyOrgs(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.LegacyMemberships, err = loadLegacyMemberships(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Users, err = loadUsers(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Projects, err = loadProjects(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Personas, err = loadPersonas(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Secrets, err = loadSecrets(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.LLMCredentials, err = loadLLMCredentials(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.LLMRoutes, err = loadLLMRoutes(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.ToolProviderConfigs, err = loadToolProviderConfigs(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.ToolDescriptionOverrides, err = loadToolDescriptionOverrides(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Threads, err = loadThreads(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Messages, err = loadMessages(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.Runs, err = loadRuns(ctx, pool); err != nil {
		return Manifest{}, err
	}
	if manifest.RunEvents, err = loadRunEvents(ctx, pool); err != nil {
		return Manifest{}, err
	}

	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func loadLegacyShapeSummary(ctx context.Context, pool *pgxpool.Pool) (LegacyShapeSummary, error) {
	summary := LegacyShapeSummary{}

	var teamCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM teams`).Scan(&teamCount); err != nil {
		return summary, fmt.Errorf("count teams: %w", err)
	}
	summary.TeamCount = teamCount

	var teamMembershipCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM team_memberships`).Scan(&teamMembershipCount); err != nil {
		return summary, fmt.Errorf("count team memberships: %w", err)
	}
	summary.TeamMembershipCount = teamMembershipCount

	rows, err := pool.Query(ctx, `
		SELECT o.id::text,
		       o.owner_user_id::text,
		       COUNT(m.user_id) AS membership_count,
		       EXISTS (
		           SELECT 1
		           FROM projects p
		           WHERE p.org_id = o.id
		             AND p.deleted_at IS NULL
		             AND p.is_default = TRUE
		             AND p.owner_user_id = o.owner_user_id
		       ) AS has_default_project
		FROM orgs o
		LEFT JOIN org_memberships m ON m.org_id = o.id
		WHERE o.deleted_at IS NULL
		GROUP BY o.id, o.owner_user_id
	`)
	if err != nil {
		return summary, fmt.Errorf("query org summary: %w", err)
	}
	defer rows.Close()

	owners := map[string]struct{}{}
	for rows.Next() {
		var orgID string
		var ownerUserID *string
		var membershipCount int
		var hasDefaultProject bool
		if err := rows.Scan(&orgID, &ownerUserID, &membershipCount, &hasDefaultProject); err != nil {
			return summary, fmt.Errorf("scan org summary: %w", err)
		}
		if ownerUserID == nil || stringsTrim(*ownerUserID) == "" {
			summary.MissingOwnerOrgIDs = append(summary.MissingOwnerOrgIDs, orgID)
		} else {
			owners[*ownerUserID] = struct{}{}
		}
		if membershipCount > 1 {
			summary.MultiMemberOrgIDs = append(summary.MultiMemberOrgIDs, orgID)
		}
		if !hasDefaultProject {
			summary.MissingDefaultProjectOrgIDs = append(summary.MissingDefaultProjectOrgIDs, orgID)
		}
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("iterate org summary: %w", err)
	}
	summary.DistinctOwnerCount = len(owners)
	return summary, nil
}

func loadLegacyOrgMappings(ctx context.Context, pool *pgxpool.Pool) ([]LegacyOrgMapping, error) {
	rows, err := pool.Query(ctx, `
		SELECT o.id::text, o.owner_user_id::text, p.id::text
		FROM orgs o
		JOIN projects p
		  ON p.org_id = o.id
		 AND p.deleted_at IS NULL
		 AND p.is_default = TRUE
		 AND p.owner_user_id = o.owner_user_id
		WHERE o.deleted_at IS NULL
		ORDER BY o.created_at ASC, p.created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query legacy org mappings: %w", err)
	}
	defer rows.Close()

	items := []LegacyOrgMapping{}
	for rows.Next() {
		var item LegacyOrgMapping
		if err := rows.Scan(&item.OrgID, &item.OwnerUserID, &item.DefaultProjectID); err != nil {
			return nil, fmt.Errorf("scan legacy org mappings: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadLegacyOrgs(ctx context.Context, pool *pgxpool.Pool) ([]LegacyOrg, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, slug, name, type, owner_user_id::text, status, country, timezone, logo_url, settings_json, created_at
		FROM orgs
		WHERE deleted_at IS NULL
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query legacy orgs: %w", err)
	}
	defer rows.Close()

	items := []LegacyOrg{}
	for rows.Next() {
		var item LegacyOrg
		var settingsJSON []byte
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Type, &item.OwnerUserID, &item.Status, &item.Country, &item.Timezone, &item.LogoURL, &settingsJSON, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan legacy org: %w", err)
		}
		item.SettingsJSON = normalizeJSON(settingsJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadLegacyMemberships(ctx context.Context, pool *pgxpool.Pool) ([]LegacyMembership, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, user_id::text, role, created_at
		FROM org_memberships
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query legacy memberships: %w", err)
	}
	defer rows.Close()

	items := []LegacyMembership{}
	for rows.Next() {
		var item LegacyMembership
		if err := rows.Scan(&item.ID, &item.OrgID, &item.UserID, &item.Role, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan legacy membership: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadUsers(ctx context.Context, pool *pgxpool.Pool) ([]UserRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT u.id::text, u.username, u.email, u.email_verified_at, u.status, u.deleted_at,
		       u.avatar_url, u.locale, u.timezone, u.last_login_at, u.tokens_invalid_before,
		       u.created_at,
		       EXISTS (
		           SELECT 1
		           FROM org_memberships m
		           WHERE m.user_id = u.id
		             AND m.role = 'platform_admin'
		       ) AS is_platform_admin
		FROM users u
		ORDER BY u.created_at ASC, u.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	items := []UserRecord{}
	for rows.Next() {
		var item UserRecord
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.EmailVerifiedAt, &item.Status, &item.DeletedAt, &item.AvatarURL, &item.Locale, &item.Timezone, &item.LastLoginAt, &item.TokensInvalidBefore, &item.CreatedAt, &item.IsPlatformAdmin); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadProjects(ctx context.Context, pool *pgxpool.Pool) ([]ProjectRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, owner_user_id::text, name, description, visibility, is_default, created_at, updated_at
		FROM projects
		WHERE deleted_at IS NULL
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	items := []ProjectRecord{}
	for rows.Next() {
		var item ProjectRecord
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.OwnerUserID, &item.Name, &item.Description, &item.Visibility, &item.IsDefault, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadPersonas(ctx context.Context, pool *pgxpool.Pool) ([]PersonaRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, persona_key, version, display_name, description,
		       soul_md, user_selectable, selector_name, selector_order,
		       prompt_md, tool_allowlist, tool_denylist, budgets_json, title_summarize_json,
		       is_active, created_at, updated_at, preferred_credential, model, reasoning_mode,
		       prompt_cache_control, executor_type, executor_config_json, sync_mode,
		       mirrored_file_dir, last_synced_at
		FROM personas
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query personas: %w", err)
	}
	defer rows.Close()

	items := []PersonaRecord{}
	for rows.Next() {
		var item PersonaRecord
		var budgetsJSON []byte
		var titleSummarizeJSON []byte
		var executorConfigJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.LegacyOrgID,
			&item.PersonaKey,
			&item.Version,
			&item.DisplayName,
			&item.Description,
			&item.SoulMD,
			&item.UserSelectable,
			&item.SelectorName,
			&item.SelectorOrder,
			&item.PromptMD,
			&item.ToolAllowlist,
			&item.ToolDenylist,
			&budgetsJSON,
			&titleSummarizeJSON,
			&item.IsActive,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.PreferredCredential,
			&item.Model,
			&item.ReasoningMode,
			&item.PromptCacheControl,
			&item.ExecutorType,
			&executorConfigJSON,
			&item.SyncMode,
			&item.MirroredFileDir,
			&item.LastSyncedAt,
		); err != nil {
			return nil, fmt.Errorf("scan persona: %w", err)
		}
		item.BudgetsJSON = normalizeJSON(budgetsJSON, `{}`)
		item.TitleSummarizeJSON = normalizeJSON(titleSummarizeJSON, `null`)
		item.ExecutorConfigJSON = normalizeJSON(executorConfigJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadSecrets(ctx context.Context, pool *pgxpool.Pool) ([]SecretRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, scope, name, encrypted_value, key_version, created_at, updated_at, rotated_at
		FROM secrets
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query secrets: %w", err)
	}
	defer rows.Close()

	items := []SecretRecord{}
	for rows.Next() {
		var item SecretRecord
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.LegacyScope, &item.Name, &item.EncryptedValue, &item.KeyVersion, &item.CreatedAt, &item.UpdatedAt, &item.RotatedAt); err != nil {
			return nil, fmt.Errorf("scan secret: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadLLMCredentials(ctx context.Context, pool *pgxpool.Pool) ([]LLMCredentialRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, scope, provider, name, secret_id::text, key_prefix,
		       base_url, openai_api_mode, advanced_json, revoked_at, last_used_at, created_at, updated_at
		FROM llm_credentials
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query llm credentials: %w", err)
	}
	defer rows.Close()

	items := []LLMCredentialRecord{}
	for rows.Next() {
		var item LLMCredentialRecord
		var advancedJSON []byte
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.LegacyScope, &item.Provider, &item.Name, &item.SecretID, &item.KeyPrefix, &item.BaseURL, &item.OpenAIAPIMode, &advancedJSON, &item.RevokedAt, &item.LastUsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan llm credential: %w", err)
		}
		item.AdvancedJSON = normalizeJSON(advancedJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadLLMRoutes(ctx context.Context, pool *pgxpool.Pool) ([]LLMRouteRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, credential_id::text, model, priority, is_default, tags,
		       when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output,
		       cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		FROM llm_routes
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query llm routes: %w", err)
	}
	defer rows.Close()

	items := []LLMRouteRecord{}
	for rows.Next() {
		var item LLMRouteRecord
		var whenJSON []byte
		var advancedJSON []byte
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.CredentialID, &item.Model, &item.Priority, &item.IsDefault, &item.Tags, &whenJSON, &advancedJSON, &item.Multiplier, &item.CostPer1kInput, &item.CostPer1kOutput, &item.CostPer1kCacheWrite, &item.CostPer1kCacheRead, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan llm route: %w", err)
		}
		item.RouteKey = item.ID
		item.WhenJSON = normalizeJSON(whenJSON, `{}`)
		item.AdvancedJSON = normalizeJSON(advancedJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadToolProviderConfigs(ctx context.Context, pool *pgxpool.Pool) ([]ToolProviderConfigRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, scope, group_name, provider_name, is_active,
		       secret_id::text, key_prefix, base_url, config_json, created_at, updated_at
		FROM tool_provider_configs
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query tool provider configs: %w", err)
	}
	defer rows.Close()

	items := []ToolProviderConfigRecord{}
	for rows.Next() {
		var item ToolProviderConfigRecord
		var configJSON []byte
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.LegacyScope, &item.GroupName, &item.ProviderName, &item.IsActive, &item.SecretID, &item.KeyPrefix, &item.BaseURL, &configJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tool provider config: %w", err)
		}
		item.ConfigJSON = normalizeJSON(configJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadToolDescriptionOverrides(ctx context.Context, pool *pgxpool.Pool) ([]ToolDescriptionOverrideRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT org_id::text, scope, tool_name, description, is_disabled, updated_at
		FROM tool_description_overrides
		ORDER BY updated_at ASC, tool_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query tool description overrides: %w", err)
	}
	defer rows.Close()

	items := []ToolDescriptionOverrideRecord{}
	for rows.Next() {
		var item ToolDescriptionOverrideRecord
		if err := rows.Scan(&item.LegacyOrgID, &item.LegacyScope, &item.ToolName, &item.Description, &item.IsDisabled, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tool description override: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadThreads(ctx context.Context, pool *pgxpool.Pool) ([]ThreadRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, project_id::text, created_by_user_id::text, title, created_at,
		       deleted_at, is_private, expires_at, parent_thread_id::text,
		       branched_from_message_id::text, title_locked
		FROM threads
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	items := []ThreadRecord{}
	for rows.Next() {
		var item ThreadRecord
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.ProjectID, &item.CreatedByUserID, &item.Title, &item.CreatedAt, &item.DeletedAt, &item.IsPrivate, &item.ExpiresAt, &item.ParentThreadID, &item.BranchedFromMessageID, &item.TitleLocked); err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadMessages(ctx context.Context, pool *pgxpool.Pool) ([]MessageRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, org_id::text, thread_id::text, created_by_user_id::text, role, content,
		       content_json, metadata_json, token_count, deleted_at, created_at, hidden
		FROM messages
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	items := []MessageRecord{}
	for rows.Next() {
		var item MessageRecord
		var contentJSON []byte
		var metadataJSON []byte
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.ThreadID, &item.CreatedByUserID, &item.Role, &item.Content, &contentJSON, &metadataJSON, &item.TokenCount, &item.DeletedAt, &item.CreatedAt, &item.Hidden); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		item.ContentJSON = normalizeJSON(contentJSON, `null`)
		item.MetadataJSON = normalizeJSON(metadataJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadRuns(ctx context.Context, pool *pgxpool.Pool) ([]RunRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT r.id::text, r.org_id::text, t.project_id::text, r.thread_id::text, r.created_by_user_id::text,
		       r.status, r.created_at, r.parent_run_id::text, r.status_updated_at,
		       r.completed_at, r.failed_at, r.duration_ms, r.total_input_tokens,
		       r.total_output_tokens, r.total_cost_usd, r.model, r.persona_id,
		       r.profile_ref, r.workspace_ref, r.deleted_at, r.environment_json
		FROM runs r
		JOIN threads t ON t.id = r.thread_id
		ORDER BY r.created_at ASC, r.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()

	items := []RunRecord{}
	for rows.Next() {
		var item RunRecord
		var environmentJSON []byte
		if err := rows.Scan(&item.ID, &item.LegacyOrgID, &item.ProjectID, &item.ThreadID, &item.CreatedByUserID, &item.Status, &item.CreatedAt, &item.ParentRunID, &item.StatusUpdatedAt, &item.CompletedAt, &item.FailedAt, &item.DurationMs, &item.TotalInputTokens, &item.TotalOutputTokens, &item.TotalCostUSD, &item.Model, &item.PersonaID, &item.ProfileRef, &item.WorkspaceRef, &item.DeletedAt, &environmentJSON); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		item.EnvironmentJSON = normalizeJSON(environmentJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadRunEvents(ctx context.Context, pool *pgxpool.Pool) ([]RunEventRecord, error) {
	rows, err := pool.Query(ctx, `
		SELECT event_id::text, run_id::text, seq, ts, type, data_json, tool_name, error_class
		FROM run_events
		ORDER BY ts ASC, event_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query run events: %w", err)
	}
	defer rows.Close()

	items := []RunEventRecord{}
	for rows.Next() {
		var item RunEventRecord
		var dataJSON []byte
		if err := rows.Scan(&item.EventID, &item.RunID, &item.Seq, &item.TS, &item.Type, &dataJSON, &item.ToolName, &item.ErrorClass); err != nil {
			return nil, fmt.Errorf("scan run event: %w", err)
		}
		item.DataJSON = normalizeJSON(dataJSON, `{}`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeJSON(raw []byte, fallback string) json.RawMessage {
	cleaned := bytes.TrimSpace(raw)
	if len(cleaned) == 0 {
		return json.RawMessage(fallback)
	}
	copyRaw := make([]byte, len(cleaned))
	copy(copyRaw, cleaned)
	return json.RawMessage(copyRaw)
}

func stringsTrim(value string) string {
	return string(bytes.TrimSpace([]byte(value)))
}

func parseUUIDPtr(raw *string) (*uuid.UUID, error) {
	if raw == nil || stringsTrim(*raw) == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(*raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
