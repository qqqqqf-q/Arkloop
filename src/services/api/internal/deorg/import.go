package deorg

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Import(ctx context.Context, pool *pgxpool.Pool, manifest Manifest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if err := ValidateManifest(manifest); err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	defaultProjects := make(map[string]string, len(manifest.LegacyOrgMappings))
	ownerUsers := make(map[string]string, len(manifest.LegacyOrgMappings))
	for _, item := range manifest.LegacyOrgMappings {
		defaultProjects[item.OrgID] = item.DefaultProjectID
		ownerUsers[item.OrgID] = item.OwnerUserID
	}

	if err := importUsers(ctx, tx, manifest.Users); err != nil {
		return err
	}
	if err := importLegacyOrgs(ctx, tx, manifest.LegacyOrgs); err != nil {
		return err
	}
	if err := importLegacyMemberships(ctx, tx, manifest.LegacyMemberships); err != nil {
		return err
	}
	if err := importProjects(ctx, tx, manifest.Projects); err != nil {
		return err
	}
	if err := importPersonas(ctx, tx, manifest.Personas, defaultProjects); err != nil {
		return err
	}
	if err := importSecrets(ctx, tx, manifest.Secrets, ownerUsers); err != nil {
		return err
	}
	if err := importLLMCredentials(ctx, tx, manifest.LLMCredentials, ownerUsers); err != nil {
		return err
	}
	if err := importLLMRoutes(ctx, tx, manifest.LLMRoutes, defaultProjects); err != nil {
		return err
	}
	if err := importToolProviderConfigs(ctx, tx, manifest.ToolProviderConfigs, defaultProjects); err != nil {
		return err
	}
	if err := importToolDescriptionOverrides(ctx, tx, manifest.ToolDescriptionOverrides, defaultProjects); err != nil {
		return err
	}
	if err := importThreads(ctx, tx, manifest.Threads); err != nil {
		return err
	}
	if err := importMessages(ctx, tx, manifest.Messages); err != nil {
		return err
	}
	if err := importRuns(ctx, tx, manifest.Runs); err != nil {
		return err
	}
	if err := importRunEvents(ctx, tx, manifest.RunEvents); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func importUsers(ctx context.Context, tx pgx.Tx, items []UserRecord) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse user id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO users (
			    id, username, email, email_verified_at, status, deleted_at,
			    avatar_url, locale, timezone, last_login_at, tokens_invalid_before,
			    created_at, is_platform_admin
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO UPDATE SET
			    username = EXCLUDED.username,
			    email = EXCLUDED.email,
			    email_verified_at = EXCLUDED.email_verified_at,
			    status = EXCLUDED.status,
			    deleted_at = EXCLUDED.deleted_at,
			    avatar_url = EXCLUDED.avatar_url,
			    locale = EXCLUDED.locale,
			    timezone = EXCLUDED.timezone,
			    last_login_at = EXCLUDED.last_login_at,
			    tokens_invalid_before = EXCLUDED.tokens_invalid_before,
			    is_platform_admin = EXCLUDED.is_platform_admin
		`, id, item.Username, item.Email, item.EmailVerifiedAt, item.Status, item.DeletedAt, item.AvatarURL, item.Locale, item.Timezone, item.LastLoginAt, item.TokensInvalidBefore, item.CreatedAt, item.IsPlatformAdmin)
		if err != nil {
			return fmt.Errorf("import user %s: %w", item.ID, err)
		}
	}
	return nil
}

func importLegacyOrgs(ctx context.Context, tx pgx.Tx, items []LegacyOrg) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse org id: %w", err)
		}
		ownerUserID, err := parseUUIDPtr(item.OwnerUserID)
		if err != nil {
			return fmt.Errorf("parse org owner_user_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO orgs (id, slug, name, type, owner_user_id, status, country, timezone, logo_url, settings_json, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11)
			ON CONFLICT (id) DO UPDATE SET
			    slug = EXCLUDED.slug,
			    name = EXCLUDED.name,
			    type = EXCLUDED.type,
			    owner_user_id = EXCLUDED.owner_user_id,
			    status = EXCLUDED.status,
			    country = EXCLUDED.country,
			    timezone = EXCLUDED.timezone,
			    logo_url = EXCLUDED.logo_url,
			    settings_json = EXCLUDED.settings_json
		`, id, item.Slug, item.Name, item.Type, ownerUserID, item.Status, item.Country, item.Timezone, item.LogoURL, jsonTextArg(item.SettingsJSON, `{}`), item.CreatedAt)
		if err != nil {
			return fmt.Errorf("import org %s: %w", item.ID, err)
		}
	}
	return nil
}

func importLegacyMemberships(ctx context.Context, tx pgx.Tx, items []LegacyMembership) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse membership id: %w", err)
		}
		orgID, err := uuid.Parse(item.OrgID)
		if err != nil {
			return fmt.Errorf("parse membership org_id: %w", err)
		}
		userID, err := uuid.Parse(item.UserID)
		if err != nil {
			return fmt.Errorf("parse membership user_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO org_memberships (id, org_id, user_id, role, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
			    role = EXCLUDED.role
		`, id, orgID, userID, item.Role, item.CreatedAt)
		if err != nil {
			return fmt.Errorf("import membership %s: %w", item.ID, err)
		}
	}
	return nil
}

func importProjects(ctx context.Context, tx pgx.Tx, items []ProjectRecord) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse project id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse project org_id: %w", err)
		}
		ownerUserID, err := parseUUIDPtr(item.OwnerUserID)
		if err != nil {
			return fmt.Errorf("parse project owner_user_id: %w", err)
		}
		updatedAt := item.CreatedAt
		if item.UpdatedAt != nil {
			updatedAt = *item.UpdatedAt
		}
		visibility := "private"
		_, err = tx.Exec(ctx, `
			INSERT INTO projects (id, org_id, team_id, owner_user_id, name, description, visibility, is_default, created_at, updated_at)
			VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    owner_user_id = EXCLUDED.owner_user_id,
			    name = EXCLUDED.name,
			    description = EXCLUDED.description,
			    visibility = EXCLUDED.visibility,
			    is_default = EXCLUDED.is_default,
			    updated_at = EXCLUDED.updated_at
		`, id, orgID, ownerUserID, item.Name, item.Description, visibility, item.IsDefault, item.CreatedAt, updatedAt)
		if err != nil {
			return fmt.Errorf("import project %s: %w", item.ID, err)
		}
	}
	return nil
}

func importPersonas(ctx context.Context, tx pgx.Tx, items []PersonaRecord, defaultProjects map[string]string) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse persona id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse persona org_id: %w", err)
		}
		projectID, err := defaultProjectUUID(item.LegacyOrgID, defaultProjects)
		if err != nil {
			return fmt.Errorf("resolve persona project_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO personas (
			    id, org_id, project_id, persona_key, version, display_name, description,
			    soul_md, user_selectable, selector_name, selector_order,
			    prompt_md, tool_allowlist, tool_denylist, budgets_json, title_summarize_json,
			    is_active, created_at, updated_at, preferred_credential, model, reasoning_mode,
			    prompt_cache_control, executor_type, executor_config_json, sync_mode,
			    mirrored_file_dir, last_synced_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb, $26, $27, $28)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    project_id = EXCLUDED.project_id,
			    display_name = EXCLUDED.display_name,
			    description = EXCLUDED.description,
			    soul_md = EXCLUDED.soul_md,
			    user_selectable = EXCLUDED.user_selectable,
			    selector_name = EXCLUDED.selector_name,
			    selector_order = EXCLUDED.selector_order,
			    prompt_md = EXCLUDED.prompt_md,
			    tool_allowlist = EXCLUDED.tool_allowlist,
			    tool_denylist = EXCLUDED.tool_denylist,
			    budgets_json = EXCLUDED.budgets_json,
			    title_summarize_json = EXCLUDED.title_summarize_json,
			    is_active = EXCLUDED.is_active,
			    updated_at = EXCLUDED.updated_at,
			    preferred_credential = EXCLUDED.preferred_credential,
			    model = EXCLUDED.model,
			    reasoning_mode = EXCLUDED.reasoning_mode,
			    prompt_cache_control = EXCLUDED.prompt_cache_control,
			    executor_type = EXCLUDED.executor_type,
			    executor_config_json = EXCLUDED.executor_config_json,
			    sync_mode = EXCLUDED.sync_mode,
			    mirrored_file_dir = EXCLUDED.mirrored_file_dir,
			    last_synced_at = EXCLUDED.last_synced_at
		`, id, orgID, projectID, item.PersonaKey, item.Version, item.DisplayName, item.Description, item.SoulMD, item.UserSelectable, item.SelectorName, item.SelectorOrder, item.PromptMD, item.ToolAllowlist, item.ToolDenylist, jsonTextArg(item.BudgetsJSON, `{}`), jsonTextArg(item.TitleSummarizeJSON, `null`), item.IsActive, item.CreatedAt, item.UpdatedAt, item.PreferredCredential, item.Model, item.ReasoningMode, item.PromptCacheControl, item.ExecutorType, jsonTextArg(item.ExecutorConfigJSON, `{}`), item.SyncMode, item.MirroredFileDir, item.LastSyncedAt)
		if err != nil {
			return fmt.Errorf("import persona %s: %w", item.ID, err)
		}
	}
	return nil
}

func importSecrets(ctx context.Context, tx pgx.Tx, items []SecretRecord, ownerUsers map[string]string) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse secret id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse secret org_id: %w", err)
		}
		ownerKind, ownerUserID, err := ownerArgs(item.LegacyScope, item.LegacyOrgID, ownerUsers)
		if err != nil {
			return fmt.Errorf("resolve secret owner: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO secrets (id, org_id, scope, owner_kind, owner_user_id, name, encrypted_value, key_version, created_at, updated_at, rotated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    scope = EXCLUDED.scope,
			    owner_kind = EXCLUDED.owner_kind,
			    owner_user_id = EXCLUDED.owner_user_id,
			    name = EXCLUDED.name,
			    encrypted_value = EXCLUDED.encrypted_value,
			    key_version = EXCLUDED.key_version,
			    updated_at = EXCLUDED.updated_at,
			    rotated_at = EXCLUDED.rotated_at
		`, id, orgID, item.LegacyScope, ownerKind, ownerUserID, item.Name, item.EncryptedValue, item.KeyVersion, item.CreatedAt, item.UpdatedAt, item.RotatedAt)
		if err != nil {
			return fmt.Errorf("import secret %s: %w", item.ID, err)
		}
	}
	return nil
}

func importLLMCredentials(ctx context.Context, tx pgx.Tx, items []LLMCredentialRecord, ownerUsers map[string]string) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse llm credential id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse llm credential org_id: %w", err)
		}
		secretID, err := parseUUIDPtr(item.SecretID)
		if err != nil {
			return fmt.Errorf("parse llm credential secret_id: %w", err)
		}
		ownerKind, ownerUserID, err := ownerArgs(item.LegacyScope, item.LegacyOrgID, ownerUsers)
		if err != nil {
			return fmt.Errorf("resolve llm credential owner: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO llm_credentials (
			    id, org_id, scope, owner_kind, owner_user_id, provider, name, secret_id,
			    key_prefix, base_url, openai_api_mode, advanced_json, revoked_at,
			    last_used_at, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    scope = EXCLUDED.scope,
			    owner_kind = EXCLUDED.owner_kind,
			    owner_user_id = EXCLUDED.owner_user_id,
			    provider = EXCLUDED.provider,
			    name = EXCLUDED.name,
			    secret_id = EXCLUDED.secret_id,
			    key_prefix = EXCLUDED.key_prefix,
			    base_url = EXCLUDED.base_url,
			    openai_api_mode = EXCLUDED.openai_api_mode,
			    advanced_json = EXCLUDED.advanced_json,
			    revoked_at = EXCLUDED.revoked_at,
			    last_used_at = EXCLUDED.last_used_at,
			    updated_at = EXCLUDED.updated_at
		`, id, orgID, item.LegacyScope, ownerKind, ownerUserID, item.Provider, item.Name, secretID, item.KeyPrefix, item.BaseURL, item.OpenAIAPIMode, jsonTextArg(item.AdvancedJSON, `{}`), item.RevokedAt, item.LastUsedAt, item.CreatedAt, item.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import llm credential %s: %w", item.ID, err)
		}
	}
	return nil
}

func importLLMRoutes(ctx context.Context, tx pgx.Tx, items []LLMRouteRecord, defaultProjects map[string]string) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse llm route id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse llm route org_id: %w", err)
		}
		credentialID, err := uuid.Parse(item.CredentialID)
		if err != nil {
			return fmt.Errorf("parse llm route credential_id: %w", err)
		}
		projectID, err := defaultProjectUUID(item.LegacyOrgID, defaultProjects)
		if err != nil {
			return fmt.Errorf("resolve llm route project_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO llm_routes (
			    id, route_key, org_id, project_id, credential_id, model, priority, is_default,
			    tags, when_json, advanced_json, multiplier, cost_per_1k_input,
			    cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16, $17)
			ON CONFLICT (id) DO UPDATE SET
			    route_key = EXCLUDED.route_key,
			    org_id = EXCLUDED.org_id,
			    project_id = EXCLUDED.project_id,
			    credential_id = EXCLUDED.credential_id,
			    model = EXCLUDED.model,
			    priority = EXCLUDED.priority,
			    is_default = EXCLUDED.is_default,
			    tags = EXCLUDED.tags,
			    when_json = EXCLUDED.when_json,
			    advanced_json = EXCLUDED.advanced_json,
			    multiplier = EXCLUDED.multiplier,
			    cost_per_1k_input = EXCLUDED.cost_per_1k_input,
			    cost_per_1k_output = EXCLUDED.cost_per_1k_output,
			    cost_per_1k_cache_write = EXCLUDED.cost_per_1k_cache_write,
			    cost_per_1k_cache_read = EXCLUDED.cost_per_1k_cache_read
		`, id, routeKeyOrFallback(item), orgID, projectID, credentialID, item.Model, item.Priority, item.IsDefault, item.Tags, jsonTextArg(item.WhenJSON, `{}`), jsonTextArg(item.AdvancedJSON, `{}`), item.Multiplier, item.CostPer1kInput, item.CostPer1kOutput, item.CostPer1kCacheWrite, item.CostPer1kCacheRead, item.CreatedAt)
		if err != nil {
			return fmt.Errorf("import llm route %s: %w", item.ID, err)
		}
	}
	return nil
}

func importToolProviderConfigs(ctx context.Context, tx pgx.Tx, items []ToolProviderConfigRecord, defaultProjects map[string]string) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse tool provider config id: %w", err)
		}
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse tool provider config org_id: %w", err)
		}
		secretID, err := parseUUIDPtr(item.SecretID)
		if err != nil {
			return fmt.Errorf("parse tool provider config secret_id: %w", err)
		}
		projectID, err := defaultProjectUUID(item.LegacyOrgID, defaultProjects)
		if err != nil {
			return fmt.Errorf("resolve tool provider project_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO tool_provider_configs (
			    id, org_id, scope, project_id, group_name, provider_name, is_active,
			    secret_id, key_prefix, base_url, config_json, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    scope = EXCLUDED.scope,
			    project_id = EXCLUDED.project_id,
			    group_name = EXCLUDED.group_name,
			    provider_name = EXCLUDED.provider_name,
			    is_active = EXCLUDED.is_active,
			    secret_id = EXCLUDED.secret_id,
			    key_prefix = EXCLUDED.key_prefix,
			    base_url = EXCLUDED.base_url,
			    config_json = EXCLUDED.config_json,
			    updated_at = EXCLUDED.updated_at
		`, id, orgID, item.LegacyScope, projectID, item.GroupName, item.ProviderName, item.IsActive, secretID, item.KeyPrefix, item.BaseURL, jsonTextArg(item.ConfigJSON, `{}`), item.CreatedAt, item.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import tool provider config %s: %w", item.ID, err)
		}
	}
	return nil
}

func importToolDescriptionOverrides(ctx context.Context, tx pgx.Tx, items []ToolDescriptionOverrideRecord, defaultProjects map[string]string) error {
	for _, item := range items {
		orgID, err := parseUUIDPtr(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse tool description override org_id: %w", err)
		}
		projectID, err := defaultProjectUUID(item.LegacyOrgID, defaultProjects)
		if err != nil {
			return fmt.Errorf("resolve tool description override project_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO tool_description_overrides (org_id, scope, project_id, tool_name, description, is_disabled, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (org_id, scope, tool_name) DO UPDATE SET
			    project_id = EXCLUDED.project_id,
			    description = EXCLUDED.description,
			    is_disabled = EXCLUDED.is_disabled,
			    updated_at = EXCLUDED.updated_at
		`, orgID, item.LegacyScope, projectID, item.ToolName, item.Description, item.IsDisabled, item.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import tool description override %s: %w", item.ToolName, err)
		}
	}
	return nil
}

func importThreads(ctx context.Context, tx pgx.Tx, items []ThreadRecord) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse thread id: %w", err)
		}
		orgID, err := uuid.Parse(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse thread org_id: %w", err)
		}
		projectID, err := uuid.Parse(item.ProjectID)
		if err != nil {
			return fmt.Errorf("parse thread project_id: %w", err)
		}
		createdByUserID, err := parseUUIDPtr(item.CreatedByUserID)
		if err != nil {
			return fmt.Errorf("parse thread created_by_user_id: %w", err)
		}
		parentThreadID, err := parseUUIDPtr(item.ParentThreadID)
		if err != nil {
			return fmt.Errorf("parse thread parent_thread_id: %w", err)
		}
		branchedFromMessageID, err := parseUUIDPtr(item.BranchedFromMessageID)
		if err != nil {
			return fmt.Errorf("parse thread branched_from_message_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO threads (
			    id, org_id, created_by_user_id, title, created_at, deleted_at,
			    project_id, is_private, expires_at, parent_thread_id,
			    branched_from_message_id, title_locked
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    created_by_user_id = EXCLUDED.created_by_user_id,
			    title = EXCLUDED.title,
			    deleted_at = EXCLUDED.deleted_at,
			    project_id = EXCLUDED.project_id,
			    is_private = EXCLUDED.is_private,
			    expires_at = EXCLUDED.expires_at,
			    parent_thread_id = EXCLUDED.parent_thread_id,
			    branched_from_message_id = EXCLUDED.branched_from_message_id,
			    title_locked = EXCLUDED.title_locked
		`, id, orgID, createdByUserID, item.Title, item.CreatedAt, item.DeletedAt, projectID, item.IsPrivate, item.ExpiresAt, parentThreadID, branchedFromMessageID, item.TitleLocked)
		if err != nil {
			return fmt.Errorf("import thread %s: %w", item.ID, err)
		}
	}
	return nil
}

func importMessages(ctx context.Context, tx pgx.Tx, items []MessageRecord) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse message id: %w", err)
		}
		orgID, err := uuid.Parse(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse message org_id: %w", err)
		}
		threadID, err := uuid.Parse(item.ThreadID)
		if err != nil {
			return fmt.Errorf("parse message thread_id: %w", err)
		}
		createdByUserID, err := parseUUIDPtr(item.CreatedByUserID)
		if err != nil {
			return fmt.Errorf("parse message created_by_user_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO messages (
			    id, org_id, thread_id, created_by_user_id, role, content,
			    content_json, metadata_json, token_count, deleted_at, created_at, hidden
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    thread_id = EXCLUDED.thread_id,
			    created_by_user_id = EXCLUDED.created_by_user_id,
			    role = EXCLUDED.role,
			    content = EXCLUDED.content,
			    content_json = EXCLUDED.content_json,
			    metadata_json = EXCLUDED.metadata_json,
			    token_count = EXCLUDED.token_count,
			    deleted_at = EXCLUDED.deleted_at,
			    hidden = EXCLUDED.hidden
		`, id, orgID, threadID, createdByUserID, item.Role, item.Content, nullableJSONTextArg(item.ContentJSON), jsonTextArg(item.MetadataJSON, `{}`), item.TokenCount, item.DeletedAt, item.CreatedAt, item.Hidden)
		if err != nil {
			return fmt.Errorf("import message %s: %w", item.ID, err)
		}
	}
	return nil
}

func importRuns(ctx context.Context, tx pgx.Tx, items []RunRecord) error {
	for _, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			return fmt.Errorf("parse run id: %w", err)
		}
		orgID, err := uuid.Parse(item.LegacyOrgID)
		if err != nil {
			return fmt.Errorf("parse run org_id: %w", err)
		}
		projectID, err := uuid.Parse(item.ProjectID)
		if err != nil {
			return fmt.Errorf("parse run project_id: %w", err)
		}
		threadID, err := uuid.Parse(item.ThreadID)
		if err != nil {
			return fmt.Errorf("parse run thread_id: %w", err)
		}
		createdByUserID, err := parseUUIDPtr(item.CreatedByUserID)
		if err != nil {
			return fmt.Errorf("parse run created_by_user_id: %w", err)
		}
		parentRunID, err := parseUUIDPtr(item.ParentRunID)
		if err != nil {
			return fmt.Errorf("parse run parent_run_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO runs (
			    id, org_id, project_id, thread_id, created_by_user_id, status, created_at,
			    parent_run_id, status_updated_at, completed_at, failed_at, duration_ms,
			    total_input_tokens, total_output_tokens, total_cost_usd, model, persona_id,
			    profile_ref, workspace_ref, deleted_at, environment_json
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21::jsonb)
			ON CONFLICT (id) DO UPDATE SET
			    org_id = EXCLUDED.org_id,
			    project_id = EXCLUDED.project_id,
			    thread_id = EXCLUDED.thread_id,
			    created_by_user_id = EXCLUDED.created_by_user_id,
			    status = EXCLUDED.status,
			    parent_run_id = EXCLUDED.parent_run_id,
			    status_updated_at = EXCLUDED.status_updated_at,
			    completed_at = EXCLUDED.completed_at,
			    failed_at = EXCLUDED.failed_at,
			    duration_ms = EXCLUDED.duration_ms,
			    total_input_tokens = EXCLUDED.total_input_tokens,
			    total_output_tokens = EXCLUDED.total_output_tokens,
			    total_cost_usd = EXCLUDED.total_cost_usd,
			    model = EXCLUDED.model,
			    persona_id = EXCLUDED.persona_id,
			    profile_ref = EXCLUDED.profile_ref,
			    workspace_ref = EXCLUDED.workspace_ref,
			    deleted_at = EXCLUDED.deleted_at,
			    environment_json = EXCLUDED.environment_json
		`, id, orgID, projectID, threadID, createdByUserID, item.Status, item.CreatedAt, parentRunID, item.StatusUpdatedAt, item.CompletedAt, item.FailedAt, item.DurationMs, item.TotalInputTokens, item.TotalOutputTokens, item.TotalCostUSD, item.Model, item.PersonaID, item.ProfileRef, item.WorkspaceRef, item.DeletedAt, jsonTextArg(item.EnvironmentJSON, `{}`))
		if err != nil {
			return fmt.Errorf("import run %s: %w", item.ID, err)
		}
	}
	return nil
}

func importRunEvents(ctx context.Context, tx pgx.Tx, items []RunEventRecord) error {
	for _, item := range items {
		eventID, err := uuid.Parse(item.EventID)
		if err != nil {
			return fmt.Errorf("parse run event id: %w", err)
		}
		runID, err := uuid.Parse(item.RunID)
		if err != nil {
			return fmt.Errorf("parse run event run_id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO run_events (event_id, run_id, seq, ts, type, data_json, tool_name, error_class)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
			ON CONFLICT (event_id) DO UPDATE SET
			    run_id = EXCLUDED.run_id,
			    seq = EXCLUDED.seq,
			    ts = EXCLUDED.ts,
			    type = EXCLUDED.type,
			    data_json = EXCLUDED.data_json,
			    tool_name = EXCLUDED.tool_name,
			    error_class = EXCLUDED.error_class
		`, eventID, runID, item.Seq, item.TS, item.Type, jsonTextArg(item.DataJSON, `{}`), item.ToolName, item.ErrorClass)
		if err != nil {
			return fmt.Errorf("import run event %s: %w", item.EventID, err)
		}
	}
	return nil
}

func defaultProjectUUID(legacyOrgID *string, defaultProjects map[string]string) (*uuid.UUID, error) {
	if legacyOrgID == nil || strings.TrimSpace(*legacyOrgID) == "" {
		return nil, nil
	}
	rawProjectID, ok := defaultProjects[*legacyOrgID]
	if !ok {
		return nil, fmt.Errorf("default project not found for org %s", *legacyOrgID)
	}
	parsed, err := uuid.Parse(rawProjectID)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func ownerArgs(legacyScope string, legacyOrgID *string, ownerUsers map[string]string) (string, *uuid.UUID, error) {
	if strings.TrimSpace(legacyScope) == "platform" {
		return "platform", nil, nil
	}
	if legacyOrgID == nil || strings.TrimSpace(*legacyOrgID) == "" {
		return "platform", nil, nil
	}
	rawUserID, ok := ownerUsers[*legacyOrgID]
	if !ok {
		return "", nil, fmt.Errorf("owner user not found for org %s", *legacyOrgID)
	}
	parsed, err := uuid.Parse(rawUserID)
	if err != nil {
		return "", nil, err
	}
	return "user", &parsed, nil
}

func routeKeyOrFallback(item LLMRouteRecord) string {
	if strings.TrimSpace(item.RouteKey) != "" {
		return item.RouteKey
	}
	return item.ID
}

func jsonTextArg(raw []byte, fallback string) *string {
	cleaned := bytes.TrimSpace(raw)
	if len(cleaned) == 0 {
		value := fallback
		return &value
	}
	value := string(cleaned)
	return &value
}

func nullableJSONTextArg(raw []byte) *string {
	cleaned := bytes.TrimSpace(raw)
	if len(cleaned) == 0 || bytes.Equal(cleaned, []byte("null")) {
		return nil
	}
	value := string(cleaned)
	return &value
}
