-- SQLite schema snapshot
-- Auto-generated from migration-to-latest on in-memory database.
-- Do NOT edit manually. Regenerate after adding migrations.

-- INDEXs

CREATE UNIQUE INDEX asr_credentials_platform_default_idx
    ON asr_credentials (is_default)
    WHERE owner_kind = 'platform' AND is_default = 1 AND revoked_at IS NULL;

CREATE UNIQUE INDEX asr_credentials_platform_name_idx
    ON asr_credentials (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX asr_credentials_user_default_idx
    ON asr_credentials (owner_user_id)
    WHERE owner_kind = 'user' AND is_default = 1 AND revoked_at IS NULL;

CREATE UNIQUE INDEX asr_credentials_user_name_idx
    ON asr_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE INDEX idx_account_entitlement_overrides_account_id
    ON account_entitlement_overrides(account_id);

CREATE INDEX idx_browser_state_registries_org_id ON browser_state_registries(account_id);

CREATE INDEX idx_channel_dm_threads_channel_id ON channel_dm_threads(channel_id);

CREATE INDEX idx_channel_dm_threads_channel_identity ON channel_dm_threads(channel_identity_id);

CREATE INDEX idx_channel_group_threads_channel_id ON channel_group_threads(channel_id);

CREATE INDEX idx_channel_identities_user_id ON channel_identities(user_id);

CREATE INDEX idx_channel_identity_bind_codes_user ON channel_identity_bind_codes(issued_by_user_id);

CREATE INDEX idx_channel_identity_links_channel_id
    ON channel_identity_links(channel_id);

CREATE INDEX idx_channel_identity_links_identity_id
    ON channel_identity_links(channel_identity_id);

CREATE INDEX idx_channel_message_deliveries_channel_id ON channel_message_deliveries(channel_id);

CREATE INDEX idx_channel_message_deliveries_run_id ON channel_message_deliveries(run_id);

CREATE INDEX idx_channel_message_deliveries_thread_id ON channel_message_deliveries(thread_id);

CREATE INDEX idx_channel_message_ledger_channel_id ON channel_message_ledger(channel_id);

CREATE INDEX idx_channel_message_ledger_message_id ON channel_message_ledger(message_id);

CREATE INDEX idx_channel_message_ledger_run_id ON channel_message_ledger(run_id);

CREATE INDEX idx_channel_message_ledger_sender_identity_id ON channel_message_ledger(sender_channel_identity_id);

CREATE INDEX idx_channel_message_ledger_thread_id ON channel_message_ledger(thread_id);

CREATE INDEX idx_channel_message_receipts_channel_id ON channel_message_receipts(channel_id);

CREATE INDEX idx_channels_account_id ON channels(account_id);

CREATE UNIQUE INDEX idx_default_workspace_bindings_workspace_ref
    ON default_workspace_bindings (workspace_ref);

CREATE INDEX idx_desktop_memory_entries_scope
    ON desktop_memory_entries (account_id, user_id, agent_id, scope);

CREATE INDEX idx_desktop_memory_entries_user
    ON desktop_memory_entries (account_id, user_id, agent_id);

CREATE INDEX idx_plan_entitlements_plan_id ON plan_entitlements(plan_id);

CREATE INDEX idx_platform_skill_overrides_profile
    ON profile_platform_skill_overrides (profile_ref);

CREATE INDEX idx_profile_mcp_installs_account_profile
    ON profile_mcp_installs (account_id, profile_ref);

CREATE INDEX idx_profile_registries_org_id ON profile_registries(account_id);

CREATE INDEX idx_profile_skill_installs_profile_ref
    ON profile_skill_installs (account_id, profile_ref);

CREATE INDEX idx_projects_org_id ON projects(account_id);

CREATE INDEX idx_projects_team_id ON projects(team_id) WHERE team_id IS NOT NULL;

CREATE UNIQUE INDEX idx_shell_sessions_org_profile_binding_type_unique
    ON shell_sessions (account_id, profile_ref, session_type, default_binding_key)
    WHERE default_binding_key IS NOT NULL AND state <> 'closed';

CREATE INDEX idx_shell_sessions_org_run ON shell_sessions(account_id, run_id);

CREATE INDEX idx_shell_sessions_org_run_type ON shell_sessions(account_id, run_id, session_type);

CREATE INDEX idx_shell_sessions_org_thread ON shell_sessions(account_id, thread_id);

CREATE INDEX idx_shell_sessions_org_workspace ON shell_sessions(account_id, workspace_ref);

CREATE INDEX idx_sub_agent_context_snapshots_updated_at
    ON sub_agent_context_snapshots(updated_at);

CREATE INDEX idx_sub_agent_events_run_id ON sub_agent_events(run_id) WHERE run_id IS NOT NULL;

CREATE INDEX idx_sub_agent_events_sub_agent_id_ts ON sub_agent_events(sub_agent_id, ts);

CREATE INDEX idx_sub_agent_events_type ON sub_agent_events(type);

CREATE INDEX idx_sub_agent_pending_inputs_sub_agent_id_seq
    ON sub_agent_pending_inputs(sub_agent_id, priority DESC, seq ASC);

CREATE INDEX idx_sub_agents_account_id ON sub_agents(account_id);

CREATE INDEX idx_sub_agents_current_run_id ON sub_agents(current_run_id) WHERE current_run_id IS NOT NULL;

CREATE INDEX idx_sub_agents_parent_run_id ON sub_agents(parent_run_id);

CREATE INDEX idx_sub_agents_root_run_id ON sub_agents(root_run_id);

CREATE INDEX idx_sub_agents_status ON sub_agents(status);

CREATE INDEX idx_team_memberships_user_id ON team_memberships(user_id);

CREATE INDEX idx_teams_org_id ON teams(org_id);

CREATE INDEX idx_threads_parent_thread_id ON threads(parent_thread_id) WHERE parent_thread_id IS NOT NULL;

CREATE INDEX idx_worker_registrations_heartbeat ON worker_registrations(heartbeat_at);

CREATE INDEX idx_worker_registrations_status ON worker_registrations(status);

CREATE INDEX idx_workspace_mcp_enablements_workspace
    ON workspace_mcp_enablements (account_id, workspace_ref, enabled);

CREATE INDEX idx_workspace_registries_org_id ON workspace_registries(account_id);

CREATE INDEX idx_workspace_skill_enablements_workspace_ref
    ON workspace_skill_enablements (account_id, workspace_ref);

CREATE INDEX ix_audit_logs_account_id_ts ON audit_logs(account_id, ts);

CREATE INDEX ix_audit_logs_trace_id ON audit_logs(trace_id);

CREATE INDEX ix_jobs_job_type ON jobs(job_type);

CREATE INDEX ix_jobs_status_available_at ON jobs(status, available_at);

CREATE INDEX ix_jobs_status_leased_until ON jobs(status, leased_until);

CREATE UNIQUE INDEX ux_jobs_run_execute_active_run ON jobs (json_extract(payload_json, '$.run_id')) WHERE job_type = 'run.execute' AND status IN ('queued', 'leased');

CREATE INDEX ix_llm_credentials_account_id ON llm_credentials(account_id);

CREATE INDEX ix_llm_routes_account_id ON llm_routes(account_id);

CREATE INDEX ix_llm_routes_credential_id ON llm_routes(credential_id);

CREATE INDEX ix_llm_routes_project_id
    ON llm_routes(project_id)
    WHERE project_id IS NOT NULL;

CREATE INDEX ix_messages_org_id_thread_id_created_at ON messages(account_id, thread_id, created_at);

CREATE INDEX ix_messages_thread_compacted
    ON messages (thread_id, compacted)
    WHERE deleted_at IS NULL AND compacted = 1;

CREATE INDEX ix_messages_thread_id ON messages(thread_id);

CREATE INDEX ix_org_memberships_org_id ON "account_memberships"(account_id);

CREATE INDEX ix_org_memberships_user_id ON "account_memberships"(user_id);

CREATE INDEX ix_run_events_run_seq ON run_events(run_id, seq);

CREATE INDEX ix_run_events_type ON run_events(type);

CREATE INDEX ix_runs_org_id ON runs(account_id);

CREATE INDEX ix_runs_thread_id ON runs(thread_id);

CREATE INDEX ix_thread_compaction_snapshots_thread_created_at
    ON thread_compaction_snapshots(thread_id, created_at DESC);

CREATE INDEX ix_threads_created_by_user_id ON threads(created_by_user_id);

CREATE INDEX ix_threads_deleted_at ON threads(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE INDEX ix_threads_org_id ON threads(account_id);

CREATE UNIQUE INDEX ix_tool_provider_configs_platform_group_active
    ON tool_provider_configs (group_name)
    WHERE owner_kind = 'platform' AND is_active = 1;

CREATE UNIQUE INDEX ix_tool_provider_configs_user_group_active
    ON tool_provider_configs (owner_user_id, group_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL AND is_active = 1;

CREATE UNIQUE INDEX llm_credentials_platform_name_idx
    ON llm_credentials (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX llm_credentials_user_name_idx
    ON llm_credentials (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens(user_id);

CREATE INDEX scheduled_triggers_next_fire_at_idx
    ON scheduled_triggers (next_fire_at);

CREATE INDEX scheduled_triggers_target_idx
    ON scheduled_triggers (channel_id, channel_identity_id);

CREATE UNIQUE INDEX secrets_platform_name_idx
    ON secrets (name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX secrets_user_name_idx
    ON secrets (owner_user_id, name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX tool_provider_configs_platform_provider_idx
    ON tool_provider_configs (provider_name)
    WHERE owner_kind = 'platform';

CREATE UNIQUE INDEX tool_provider_configs_user_provider_idx
    ON tool_provider_configs (owner_user_id, provider_name)
    WHERE owner_kind = 'user' AND owner_user_id IS NOT NULL;

CREATE UNIQUE INDEX uq_platform_skills
    ON skill_packages (skill_key, version)
    WHERE account_id IS NULL;

CREATE UNIQUE INDEX uq_thread_compaction_snapshots_active_thread
    ON thread_compaction_snapshots(thread_id)
    WHERE is_active = 1;

CREATE UNIQUE INDEX uq_user_credentials_login ON user_credentials(login);

CREATE UNIQUE INDEX uq_user_skills
    ON skill_packages (account_id, skill_key, version)
    WHERE account_id IS NOT NULL;

CREATE UNIQUE INDEX uq_users_email ON users (email) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX usage_records_run_id_usage_type_uidx
  ON usage_records (run_id, usage_type);

CREATE UNIQUE INDEX ux_llm_routes_credential_default
    ON llm_routes (credential_id)
    WHERE is_default = 1;

CREATE UNIQUE INDEX ux_llm_routes_credential_model_lower
    ON llm_routes (credential_id, lower(model));

CREATE UNIQUE INDEX ux_llm_routes_route_key
    ON llm_routes (lower(route_key))
    WHERE route_key IS NOT NULL;


-- TABLEs

CREATE TABLE account_entitlement_overrides (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key                TEXT NOT NULL,
    value              TEXT NOT NULL,
    value_type         TEXT NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    reason             TEXT,
    expires_at         TEXT,
    created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, key)
);

CREATE TABLE "account_memberships" (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id     TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member',
    created_at TEXT NOT NULL DEFAULT (datetime('now')), role_id TEXT,
    UNIQUE (account_id, user_id)
);

CREATE TABLE "accounts" (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'personal' CHECK (type IN ('personal', 'workspace')),
    owner_user_id TEXT,
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
, country TEXT, timezone TEXT, logo_url TEXT, settings_json TEXT NOT NULL DEFAULT '{}', deleted_at TEXT);

CREATE TABLE api_keys (
    id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id  TEXT NOT NULL,
    user_id     TEXT NOT NULL REFERENCES users(id),
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    key_prefix  TEXT NOT NULL,
    permissions TEXT,
    team_id     TEXT,
    last_used_at TEXT,
    expires_at  TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE asr_credentials (
    id            TEXT PRIMARY KEY DEFAULT (
        lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
        substr(lower(hex(randomblob(2))),2) || '-' ||
        substr('89ab',abs(random()) % 4 + 1, 1) ||
        substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id    TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    owner_kind    TEXT NOT NULL DEFAULT 'user' CHECK (owner_kind IN ('platform', 'user')),
    owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,
    name          TEXT NOT NULL,
    secret_id     TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    key_prefix    TEXT,
    base_url      TEXT,
    model         TEXT NOT NULL,
    is_default    INTEGER NOT NULL DEFAULT 0,
    revoked_at    TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    -- legacy column kept for data safety on upgrade; not read by new app code
    api_key_legacy TEXT
);

CREATE TABLE audit_logs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id   TEXT NOT NULL,
    actor_user_id      TEXT,
    action       TEXT NOT NULL,
    target_type TEXT,
    target_id  TEXT,
    metadata_json      TEXT,
    ip_address   TEXT,
    ts   TEXT NOT NULL DEFAULT (datetime('now'))
, trace_id TEXT NOT NULL DEFAULT '', user_agent TEXT, api_key_id TEXT, before_state_json TEXT, after_state_json TEXT);

CREATE TABLE browser_state_registries (
    workspace_ref           TEXT PRIMARY KEY,
    account_id                  TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    owner_user_id           TEXT,
    latest_manifest_rev     TEXT,
    lease_holder_id         TEXT,
    lease_until             TEXT,
    store_key               TEXT,
    flush_state             TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER NOT NULL DEFAULT 0,
    last_used_at            TEXT NOT NULL DEFAULT (datetime('now')),
    last_flush_failed_at    TEXT,
    last_flush_succeeded_at TEXT,
    metadata_json           TEXT NOT NULL DEFAULT '{}',
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE TABLE channel_dm_threads (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id TEXT NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    persona_id          TEXT NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, channel_identity_id, persona_id),
    UNIQUE (thread_id)
);

CREATE TABLE channel_group_threads (
    id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id       TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id TEXT NOT NULL,
    persona_id       TEXT NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    thread_id        TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, persona_id),
    UNIQUE (thread_id)
);

CREATE TABLE channel_identities (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_type        TEXT NOT NULL,
    platform_subject_id TEXT NOT NULL,
    user_id             TEXT REFERENCES users(id) ON DELETE SET NULL,
    display_name        TEXT,
    avatar_url          TEXT,
    metadata            TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')), heartbeat_enabled INTEGER NOT NULL DEFAULT 0, heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30, heartbeat_model TEXT NOT NULL DEFAULT '',
    UNIQUE (channel_type, platform_subject_id)
);

CREATE TABLE channel_identity_bind_codes (
    id                          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    token                       TEXT NOT NULL UNIQUE,
    issued_by_user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type                TEXT,
    used_at                     TEXT,
    used_by_channel_identity_id TEXT REFERENCES channel_identities(id),
    expires_at                  TEXT NOT NULL,
    created_at                  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE "channel_identity_links" (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_identity_id TEXT NOT NULL REFERENCES channel_identities(id) ON DELETE CASCADE,
    heartbeat_enabled   INTEGER NOT NULL DEFAULT 0,
    heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
    heartbeat_model     TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, channel_identity_id)
);

CREATE TABLE channel_message_deliveries (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    run_id              TEXT REFERENCES runs(id) ON DELETE SET NULL,
    thread_id           TEXT REFERENCES threads(id) ON DELETE SET NULL,
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT NOT NULL,
    platform_message_id TEXT NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE TABLE channel_message_ledger (
    id                         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id                 TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_type               TEXT NOT NULL,
    direction                  TEXT NOT NULL,
    thread_id                  TEXT REFERENCES threads(id) ON DELETE SET NULL,
    run_id                     TEXT REFERENCES runs(id) ON DELETE SET NULL,
    platform_conversation_id   TEXT NOT NULL,
    platform_message_id        TEXT NOT NULL,
    platform_parent_message_id TEXT,
    platform_thread_id         TEXT,
    sender_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    message_id                 TEXT REFERENCES messages(id) ON DELETE SET NULL,
    CHECK (direction IN ('inbound', 'outbound')),
    UNIQUE (channel_id, direction, platform_conversation_id, platform_message_id)
);

CREATE TABLE channel_message_receipts (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    platform_chat_id    TEXT NOT NULL,
    platform_message_id TEXT NOT NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (channel_id, platform_chat_id, platform_message_id)
);

CREATE TABLE channels (
    id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    channel_type   TEXT NOT NULL,
    persona_id     TEXT REFERENCES personas(id) ON DELETE SET NULL,
    credentials_id TEXT REFERENCES secrets(id),
    owner_user_id  TEXT REFERENCES users(id),
    webhook_secret TEXT,
    webhook_url    TEXT,
    is_active      INTEGER NOT NULL DEFAULT 0,
    config_json    TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, channel_type)
);

CREATE TABLE credit_transactions (
    id             TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    amount         INTEGER NOT NULL,
    type           TEXT NOT NULL,
    reference_type TEXT,
    reference_id   TEXT,
    note           TEXT,
    metadata       TEXT,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE credits (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id TEXT NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    balance    INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE default_workspace_bindings (
    profile_ref       TEXT NOT NULL,
    owner_user_id     TEXT,
    account_id            TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    binding_scope     TEXT NOT NULL CHECK (binding_scope IN ('project', 'thread')),
    binding_target_id TEXT NOT NULL,
    workspace_ref     TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, profile_ref, binding_scope, binding_target_id)
);

CREATE TABLE desktop_memory_entries (
    id         TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT 'default',
    scope      TEXT NOT NULL DEFAULT 'user',
    category   TEXT NOT NULL DEFAULT 'general',
    entry_key  TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE entitlements (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL,
    subscription_id TEXT REFERENCES subscriptions(id),
    feature_key     TEXT NOT NULL,
    value           TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE feature_flags (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    key           TEXT NOT NULL UNIQUE,
    description   TEXT,
    default_value INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE invite_codes (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    code       TEXT NOT NULL UNIQUE,
    account_id TEXT,
    max_uses   INTEGER NOT NULL DEFAULT 1,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE ip_rules (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id TEXT NOT NULL,
    cidr       TEXT NOT NULL,
    action     TEXT NOT NULL DEFAULT 'allow',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    job_type     TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'queued',
    available_at TEXT NOT NULL DEFAULT (datetime('now')),
    leased_until TEXT,
    lease_token  TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    worker_tags  TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE llm_credentials (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini', 'deepseek')),
    name            TEXT NOT NULL,
    secret_id       TEXT,
    key_prefix      TEXT,
    base_url        TEXT,
    openai_api_mode TEXT,
    advanced_json   TEXT NOT NULL DEFAULT '{}',
    revoked_at      TEXT,
    last_used_at    TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    owner_kind      TEXT NOT NULL DEFAULT 'platform',
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE llm_routes (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id             TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    project_id             TEXT REFERENCES projects(id) ON DELETE CASCADE,
    credential_id          TEXT NOT NULL REFERENCES llm_credentials(id) ON DELETE CASCADE,
    model                  TEXT NOT NULL,
    priority               INTEGER NOT NULL DEFAULT 0,
    is_default             INTEGER NOT NULL DEFAULT 0,
    tags                   TEXT NOT NULL DEFAULT '[]',
    when_json              TEXT NOT NULL DEFAULT '{}',
    advanced_json          TEXT NOT NULL DEFAULT '{}',
    multiplier             REAL NOT NULL DEFAULT 1.0,
    cost_per_1k_input      REAL,
    cost_per_1k_output     REAL,
    cost_per_1k_cache_write REAL,
    cost_per_1k_cache_read REAL,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    route_key              TEXT
, show_in_picker INTEGER NOT NULL DEFAULT 1);

CREATE TABLE mcp_configs (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id             TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    transport          TEXT NOT NULL CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    url                TEXT,
    auth_secret_id     TEXT,
    command            TEXT,
    args_json          TEXT NOT NULL DEFAULT '[]',
    cwd                TEXT,
    env_json           TEXT NOT NULL DEFAULT '{}',
    inherit_parent_env INTEGER NOT NULL DEFAULT 0,
    call_timeout_ms    INTEGER NOT NULL DEFAULT 10000 CHECK (call_timeout_ms > 0),
    is_active          INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, name)
);

CREATE TABLE messages (
    id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id          TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL,
    content_json       TEXT,
    metadata_json      TEXT NOT NULL DEFAULT '{}',
    hidden             INTEGER NOT NULL DEFAULT 0,
    deleted_at         TEXT,
    token_count        INTEGER,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    compacted          INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE notification_broadcasts (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    target_type     TEXT NOT NULL DEFAULT 'all',
    target_id       TEXT,
    payload_json    TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    sent_count      INTEGER NOT NULL DEFAULT 0,
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    deleted_at      TEXT
);

CREATE TABLE notifications (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT,
    read       INTEGER NOT NULL DEFAULT 0,
    data       TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
, account_id TEXT, payload_json TEXT NOT NULL DEFAULT '{}', read_at TEXT, broadcast_id TEXT REFERENCES notification_broadcasts(id));

CREATE TABLE personas (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id              TEXT,
    persona_key         TEXT NOT NULL,
    version             TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    description         TEXT,
    prompt_md           TEXT NOT NULL,
    tool_allowlist      TEXT NOT NULL DEFAULT '[]',
    tool_denylist       TEXT NOT NULL DEFAULT '[]',
    budgets_json        TEXT NOT NULL DEFAULT '{}',
    is_active           INTEGER NOT NULL DEFAULT 1,
    executor_type       TEXT NOT NULL DEFAULT 'agent.simple',
    executor_config_json TEXT NOT NULL DEFAULT '{}',
    preferred_credential TEXT,
    model               TEXT,
    reasoning_mode      TEXT NOT NULL DEFAULT 'auto',
    prompt_cache_control TEXT NOT NULL DEFAULT 'none',
    created_at          TEXT NOT NULL DEFAULT (datetime('now')), soul_md TEXT NOT NULL DEFAULT '', user_selectable INTEGER NOT NULL DEFAULT 0, selector_name TEXT, selector_order INTEGER, roles_json TEXT NOT NULL DEFAULT '{}', title_summarize_json TEXT, updated_at TEXT NOT NULL DEFAULT (datetime('now')), sync_mode TEXT NOT NULL DEFAULT 'none', mirrored_file_dir TEXT, last_synced_at TEXT, project_id TEXT, core_tools TEXT NOT NULL DEFAULT '[]', stream_thinking INTEGER NOT NULL DEFAULT 1, conditional_tools_json TEXT, result_summarize_json TEXT, heartbeat_enabled INTEGER NOT NULL DEFAULT 0, heartbeat_interval_minutes INTEGER NOT NULL DEFAULT 30,
    UNIQUE (account_id, persona_key, version)
);

CREATE TABLE plan_entitlements (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    plan_id    TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('int', 'bool', 'string')),
    UNIQUE (plan_id, key)
);

CREATE TABLE plans (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    features   TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE platform_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE profile_mcp_installs (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    install_key            TEXT NOT NULL,
    account_id             TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    profile_ref            TEXT NOT NULL REFERENCES profile_registries(profile_ref) ON DELETE CASCADE,
    display_name           TEXT NOT NULL,
    source_kind            TEXT NOT NULL,
    source_uri             TEXT,
    sync_mode              TEXT NOT NULL DEFAULT 'none',
    transport              TEXT NOT NULL CHECK (transport IN ('stdio', 'http_sse', 'streamable_http')),
    launch_spec_json       TEXT NOT NULL DEFAULT '{}',
    auth_headers_secret_id TEXT REFERENCES secrets(id) ON DELETE SET NULL,
    host_requirement       TEXT NOT NULL,
    discovery_status       TEXT NOT NULL DEFAULT 'needs_check',
    last_error_code        TEXT,
    last_error_message     TEXT,
    last_checked_at        TEXT,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at             TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (account_id, profile_ref, install_key)
);

CREATE TABLE profile_platform_skill_overrides (
    profile_ref TEXT    NOT NULL,
    skill_key   TEXT    NOT NULL,
    version     TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'manual',
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (profile_ref, skill_key, version),
    CHECK (status IN ('manual', 'removed'))
);

CREATE TABLE profile_registries (
    profile_ref             TEXT PRIMARY KEY,
    account_id                  TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    owner_user_id           TEXT,
    latest_manifest_rev     TEXT,
    flush_state             TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count       INTEGER NOT NULL DEFAULT 0,
    last_flush_failed_at    TEXT,
    last_flush_succeeded_at TEXT,
    lease_holder_id         TEXT,
    lease_until             TEXT,
    default_workspace_ref   TEXT,
    store_key               TEXT,
    last_used_at            TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json           TEXT NOT NULL DEFAULT '{}',
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE TABLE profile_skill_installs (
    profile_ref   TEXT NOT NULL,
    account_id        TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    skill_key     TEXT NOT NULL,
    version       TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (profile_ref, skill_key, version)
);

CREATE TABLE projects (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id        TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    team_id       TEXT REFERENCES teams(id) ON DELETE SET NULL,
    name          TEXT NOT NULL,
    description   TEXT,
    visibility    TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'team', 'org')),
    owner_user_id TEXT,
    deleted_at    TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
, is_default INTEGER NOT NULL DEFAULT 0, updated_at TEXT);

CREATE TABLE redemption_codes (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    code       TEXT NOT NULL UNIQUE,
    amount     INTEGER NOT NULL,
    max_uses   INTEGER NOT NULL DEFAULT 1,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE referrals (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    referrer_id   TEXT NOT NULL REFERENCES users(id),
    referred_id   TEXT NOT NULL REFERENCES users(id),
    invite_code_id TEXT REFERENCES invite_codes(id),
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE refresh_tokens (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    last_used_at TEXT
);

CREATE TABLE run_events (
    event_id    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    run_id      TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    ts          TEXT NOT NULL DEFAULT (datetime('now')),
    type        TEXT NOT NULL,
    data_json   TEXT NOT NULL DEFAULT '{}',
    tool_name   TEXT,
    error_class TEXT,
    UNIQUE (run_id, seq)
);

CREATE TABLE runs (
    id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id          TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id           TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    created_by_user_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'cancelling', 'interrupted')),
    next_event_seq      INTEGER NOT NULL DEFAULT 1,
    parent_run_id       TEXT REFERENCES runs(id) ON DELETE SET NULL,
    resume_from_run_id  TEXT REFERENCES runs(id) ON DELETE SET NULL,
    status_updated_at   TEXT,
    completed_at        TEXT,
    failed_at           TEXT,
    duration_ms         INTEGER,
    total_input_tokens  INTEGER,
    total_output_tokens INTEGER,
    total_cost_usd      TEXT,
    model               TEXT,
    persona_id          TEXT,
    deleted_at          TEXT,
    profile_ref         TEXT,
    workspace_ref       TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE "scheduled_triggers" (
    id                    TEXT PRIMARY KEY,
    channel_id            TEXT NOT NULL,
    channel_identity_id   TEXT NOT NULL,
    persona_key           TEXT NOT NULL,
    account_id            TEXT NOT NULL,
    model                 TEXT NOT NULL DEFAULT '',
    interval_min          INTEGER NOT NULL DEFAULT 30,
    next_fire_at          TEXT NOT NULL,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    UNIQUE (channel_id, channel_identity_id)
);

CREATE TABLE secrets (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    owner_kind      TEXT NOT NULL DEFAULT 'platform',
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    encrypted_value TEXT NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now')),
    rotated_at      TEXT
);

CREATE TABLE shell_sessions (
    session_ref         TEXT PRIMARY KEY,
    account_id              TEXT NOT NULL,
    profile_ref         TEXT NOT NULL,
    workspace_ref       TEXT NOT NULL,
    project_id          TEXT,
    thread_id           TEXT,
    run_id              TEXT,
    share_scope         TEXT NOT NULL,
    state               TEXT NOT NULL,
    live_session_id     TEXT,
    latest_restore_rev  TEXT,
    last_used_at        TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json       TEXT NOT NULL DEFAULT '{}',
    default_binding_key TEXT,
    lease_owner_id      TEXT,
    lease_until         TEXT,
    lease_epoch         INTEGER NOT NULL DEFAULT 0,
    session_type        TEXT NOT NULL DEFAULT 'shell' CHECK (session_type IN ('shell', 'browser')),
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_owner_id IS NULL AND lease_until IS NULL) OR (lease_owner_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE TABLE "skill_packages" (
    id                    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id            TEXT,                                  -- NULL = platform-owned
    skill_key             TEXT    NOT NULL,
    version               TEXT    NOT NULL,
    display_name          TEXT    NOT NULL,
    description           TEXT,
    instruction_path      TEXT    NOT NULL DEFAULT '',
    manifest_key          TEXT    NOT NULL DEFAULT '',
    bundle_key            TEXT    NOT NULL DEFAULT '',
    files_prefix          TEXT    NOT NULL DEFAULT '',
    platforms             TEXT    NOT NULL DEFAULT '[]',
    registry_provider     TEXT,
    registry_slug         TEXT,
    registry_owner_handle TEXT,
    registry_version      TEXT,
    registry_detail_url   TEXT,
    registry_download_url TEXT,
    registry_source_kind  TEXT,
    registry_source_url   TEXT,
    scan_status           TEXT    NOT NULL DEFAULT 'unknown',
    scan_has_warnings     INTEGER NOT NULL DEFAULT 0,
    scan_checked_at       TEXT,
    scan_engine           TEXT,
    scan_summary          TEXT,
    moderation_verdict    TEXT,
    scan_snapshot_json    TEXT,
    sync_mode             TEXT    NOT NULL DEFAULT 'none',
    content_hash          TEXT,
    is_active             INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE smtp_providers (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    host       TEXT NOT NULL,
    port       INTEGER NOT NULL DEFAULT 587,
    username   TEXT NOT NULL,
    password   TEXT NOT NULL,
    from_email TEXT NOT NULL,
    from_name  TEXT,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE sub_agent_context_snapshots (
    sub_agent_id  TEXT PRIMARY KEY REFERENCES sub_agents(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE sub_agent_events (
    event_id      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    sub_agent_id  TEXT NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    run_id        TEXT REFERENCES runs(id) ON DELETE SET NULL,
    seq           INTEGER NOT NULL,
    ts            TEXT NOT NULL DEFAULT (datetime('now')),
    type          TEXT NOT NULL,
    data_json     TEXT NOT NULL DEFAULT '{}',
    error_class   TEXT,
    UNIQUE (sub_agent_id, seq)
);

CREATE TABLE sub_agent_pending_inputs (
    id           TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    sub_agent_id TEXT NOT NULL REFERENCES sub_agents(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    input        TEXT NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (sub_agent_id, seq)
);

CREATE TABLE sub_agents (
    id                    TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id            TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    parent_run_id         TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    parent_thread_id      TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    root_run_id           TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    root_thread_id        TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depth                 INTEGER NOT NULL CHECK (depth >= 0),
    role                  TEXT,
    persona_id            TEXT,
    nickname              TEXT,
    source_type           TEXT NOT NULL,
    context_mode          TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (
        status IN (
            'created',
            'queued',
            'running',
            'waiting_input',
            'completed',
            'failed',
            'cancelled',
            'closed',
            'resumable'
        )
    ),
    current_run_id        TEXT REFERENCES runs(id) ON DELETE SET NULL,
    last_completed_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    last_output_ref       TEXT,
    last_error            TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    started_at            TEXT,
    completed_at          TEXT,
    closed_at             TEXT
);

CREATE TABLE subscriptions (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id TEXT NOT NULL,
    plan_id    TEXT NOT NULL REFERENCES plans(id),
    status     TEXT NOT NULL DEFAULT 'active',
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
, current_period_start TEXT NOT NULL DEFAULT (datetime('now')), current_period_end   TEXT NOT NULL DEFAULT (datetime('now', '+100 years')), cancelled_at         TEXT);

CREATE TABLE team_memberships (
    team_id    TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'member',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (team_id, user_id)
);

CREATE TABLE teams (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    org_id     TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE thread_compaction_snapshots (
    id                     TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id             TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id              TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    summary_text           TEXT NOT NULL,
    metadata_json          TEXT NOT NULL DEFAULT '{}',
    supersedes_snapshot_id TEXT REFERENCES thread_compaction_snapshots(id) ON DELETE SET NULL,
    is_active              INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    created_at             TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE thread_reports (
    id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id   TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    reporter_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE thread_shares (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    token      TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE thread_stars (
    id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    thread_id  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(thread_id, user_id)
);

CREATE TABLE "threads" (
    id                       TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id               TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_by_user_id       TEXT REFERENCES users(id) ON DELETE SET NULL,
    title                    TEXT,
    project_id               TEXT,
    deleted_at               TEXT,
    is_private               INTEGER NOT NULL DEFAULT 0,
    expires_at               TEXT,
    parent_thread_id         TEXT REFERENCES threads(id) ON DELETE SET NULL,
    branched_from_message_id TEXT,
    title_locked             INTEGER NOT NULL DEFAULT 0,
    mode                     TEXT NOT NULL DEFAULT 'chat' CHECK (mode IN ('chat', 'work')),
    created_at               TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (id, account_id)
);

CREATE TABLE tool_description_overrides (
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope       TEXT NOT NULL DEFAULT 'platform',
    tool_name   TEXT NOT NULL,
    description TEXT NOT NULL,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')), project_id TEXT,
    PRIMARY KEY (account_id, scope, tool_name)
);

CREATE TABLE tool_provider_configs (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id      TEXT NOT NULL DEFAULT '00000000-0000-4000-8000-000000000002' REFERENCES accounts(id) ON DELETE CASCADE,
    owner_kind      TEXT NOT NULL DEFAULT 'platform' CHECK (owner_kind IN ('platform', 'user')),
    owner_user_id   TEXT REFERENCES users(id) ON DELETE CASCADE,
    group_name      TEXT NOT NULL,
    provider_name   TEXT NOT NULL,
    is_active       INTEGER NOT NULL DEFAULT 0,
    secret_id       TEXT,
    key_prefix      TEXT,
    base_url        TEXT,
    config_json     TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE usage_records (
    id            TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id    TEXT NOT NULL,
    user_id       TEXT,
    feature_key   TEXT NOT NULL,
    quantity      INTEGER NOT NULL DEFAULT 1,
    metadata      TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
, run_id TEXT, input_tokens INTEGER, output_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER, cached_tokens INTEGER, model TEXT NOT NULL DEFAULT '', usage_type TEXT NOT NULL DEFAULT 'llm', cost_usd REAL NOT NULL DEFAULT 0);

CREATE TABLE user_credentials (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    login           TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE user_memory_snapshots (
    account_id       TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    agent_id     TEXT NOT NULL DEFAULT 'default',
    memory_block TEXT NOT NULL,
    hits_json    TEXT,
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, user_id, agent_id)
);

CREATE TABLE user_notebook_snapshots (
    account_id     TEXT NOT NULL,
    user_id        TEXT NOT NULL,
    agent_id       TEXT NOT NULL DEFAULT 'default',
    notebook_block TEXT NOT NULL,
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, user_id, agent_id)
);

CREATE TABLE users (
    id                TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    username          TEXT NOT NULL,
    email             TEXT,
    email_verified_at TEXT,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
    deleted_at        TEXT,
    avatar_url        TEXT,
    locale            TEXT,
    timezone          TEXT,
    last_login_at     TEXT,
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
, tokens_invalid_before TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z', source TEXT NOT NULL DEFAULT 'web');

CREATE TABLE webhooks (
    id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    account_id  TEXT NOT NULL,
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL,
    events      TEXT,
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE worker_registrations (
    id              TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
    worker_id       TEXT NOT NULL UNIQUE,
    hostname        TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT 'unknown',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'draining', 'dead')),
    capabilities    TEXT NOT NULL DEFAULT '[]',
    current_load    INTEGER NOT NULL DEFAULT 0,
    max_concurrency INTEGER NOT NULL DEFAULT 4,
    heartbeat_at    TEXT NOT NULL DEFAULT (datetime('now')),
    registered_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE workspace_mcp_enablements (
    workspace_ref       TEXT NOT NULL REFERENCES workspace_registries(workspace_ref) ON DELETE CASCADE,
    account_id          TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    install_id          TEXT NOT NULL REFERENCES profile_mcp_installs(id) ON DELETE CASCADE,
    install_key         TEXT NOT NULL,
    enabled_by_user_id  TEXT REFERENCES users(id) ON DELETE SET NULL,
    enabled             INTEGER NOT NULL DEFAULT 0,
    enabled_at          TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_ref, install_id)
);

CREATE TABLE workspace_registries (
    workspace_ref               TEXT PRIMARY KEY,
    account_id                      TEXT NOT NULL REFERENCES "accounts"(id) ON DELETE CASCADE,
    owner_user_id               TEXT,
    project_id                  TEXT,
    latest_manifest_rev         TEXT,
    flush_state                 TEXT NOT NULL DEFAULT 'idle' CHECK (flush_state IN ('idle', 'pending', 'running', 'failed')),
    flush_retry_count           INTEGER NOT NULL DEFAULT 0,
    last_flush_failed_at        TEXT,
    last_flush_succeeded_at     TEXT,
    lease_holder_id             TEXT,
    lease_until                 TEXT,
    default_shell_session_ref   TEXT,
    store_key                   TEXT,
    last_used_at                TEXT NOT NULL DEFAULT (datetime('now')),
    metadata_json               TEXT NOT NULL DEFAULT '{}',
    created_at                  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                  TEXT NOT NULL DEFAULT (datetime('now')),
    CHECK ((lease_holder_id IS NULL AND lease_until IS NULL) OR (lease_holder_id IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE TABLE workspace_skill_enablements (
    workspace_ref      TEXT NOT NULL,
    account_id             TEXT NOT NULL,
    enabled_by_user_id TEXT NOT NULL,
    skill_key          TEXT NOT NULL,
    version            TEXT NOT NULL,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (workspace_ref, skill_key)
);
