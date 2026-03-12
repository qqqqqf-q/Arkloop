package environmentbindings

import (
"context"
"encoding/json"
"fmt"
"sort"
"strings"
"time"

"arkloop/services/worker/internal/data"

"github.com/google/uuid"
"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
)

func ResolveAndPersistRun(ctx context.Context, pool *pgxpool.Pool, run data.Run) (data.Run, error) {
if ctx == nil {
ctx = context.Background()
}
if pool == nil {
return run, fmt.Errorf("pool must not be nil")
}
if run.ID == uuid.Nil || run.OrgID == uuid.Nil || run.ThreadID == uuid.Nil {
return run, fmt.Errorf("run_id, org_id, thread_id must not be empty")
}
if run.CreatedByUserID == nil || *run.CreatedByUserID == uuid.Nil {
return run, nil
}
if strings.TrimSpace(ptrString(run.ProfileRef)) != "" && strings.TrimSpace(ptrString(run.WorkspaceRef)) != "" {
return run, nil
}

tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
if err != nil {
return run, err
}
defer tx.Rollback(ctx)

runsRepo := data.RunsRepository{}
if err := runsRepo.LockRunRow(ctx, tx, run.ID); err != nil {
return run, err
}
storedRun, err := runsRepo.GetRun(ctx, tx, run.ID)
if err != nil {
return run, err
}
if storedRun == nil {
return run, fmt.Errorf("run not found: %s", run.ID)
}
if strings.TrimSpace(ptrString(storedRun.ProfileRef)) != "" && strings.TrimSpace(ptrString(storedRun.WorkspaceRef)) != "" {
if err := tx.Commit(ctx); err != nil {
return run, err
}
return *storedRun, nil
}

profileRef, err := ensureProfileRegistry(ctx, tx, *storedRun)
if err != nil {
return run, err
}
workspaceRef, err := ensureWorkspaceRegistry(ctx, tx, *storedRun, profileRef)
if err != nil {
return run, err
}
if err := runsRepo.UpdateEnvironmentBindings(ctx, tx, storedRun.ID, profileRef, workspaceRef); err != nil {
return run, err
}
storedRun.ProfileRef = stringPtr(profileRef)
storedRun.WorkspaceRef = stringPtr(workspaceRef)

if err := tx.Commit(ctx); err != nil {
return run, err
}
return *storedRun, nil
}

func ensureProfileRegistry(ctx context.Context, tx pgx.Tx, run data.Run) (string, error) {
ownerUserID := *run.CreatedByUserID
profileRef := profileRefForUser(ownerUserID)
_, err := tx.Exec(
ctx,
`INSERT INTO profile_registries (
profile_ref,
org_id,
owner_user_id,
flush_state,
flush_retry_count,
last_used_at,
metadata_json
) VALUES ($1, $2, $3, 'idle', 0, $4, '{}'::jsonb)
ON CONFLICT (profile_ref) DO UPDATE SET
owner_user_id = COALESCE(EXCLUDED.owner_user_id, profile_registries.owner_user_id),
last_used_at = EXCLUDED.last_used_at,
updated_at = now()`,
profileRef,
run.OrgID,
ownerUserID,
time.Now().UTC(),
)
if err != nil {
return "", err
}
return profileRef, nil
}

func ensureWorkspaceRegistry(ctx context.Context, tx pgx.Tx, run data.Run, profileRef string) (string, error) {
bindingScope := data.BindingScopeThread
bindingTargetID := run.ThreadID
if run.ProjectID != nil && *run.ProjectID != uuid.Nil {
bindingScope = data.BindingScopeProject
bindingTargetID = *run.ProjectID
}

workspaceRef, exists, err := lookupBoundWorkspaceRef(ctx, tx, run.OrgID, profileRef, bindingScope, bindingTargetID)
if err != nil {
return "", err
}
if exists {
if err := touchProfileDefaultWorkspace(ctx, tx, profileRef, workspaceRef); err != nil {
return "", err
}
return workspaceRef, nil
}

ownerUserID := *run.CreatedByUserID
workspaceRef = workspaceRefForBinding(profileRef, bindingScope, bindingTargetID)
if err := upsertWorkspaceRegistry(ctx, tx, workspaceRef, run.OrgID, &ownerUserID, run.ProjectID); err != nil {
return "", err
}
if err := insertWorkspaceBinding(ctx, tx, run.OrgID, &ownerUserID, profileRef, bindingScope, bindingTargetID, workspaceRef); err != nil {
return "", err
}

sourceWorkspaceRef, err := loadProfileDefaultWorkspace(ctx, tx, profileRef)
if err != nil {
return "", err
}
if sourceWorkspaceRef != "" && sourceWorkspaceRef != workspaceRef {
if err := copyWorkspaceSkills(ctx, tx, run.OrgID, ownerUserID, sourceWorkspaceRef, workspaceRef); err != nil {
return "", err
}
}
if err := syncWorkspaceMetadata(ctx, tx, workspaceRef); err != nil {
return "", err
}
if err := touchProfileDefaultWorkspace(ctx, tx, profileRef, workspaceRef); err != nil {
return "", err
}
return workspaceRef, nil
}

func lookupBoundWorkspaceRef(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, profileRef string, bindingScope string, bindingTargetID uuid.UUID) (string, bool, error) {
var workspaceRef string
err := tx.QueryRow(
ctx,
`SELECT workspace_ref
   FROM default_workspace_bindings
  WHERE org_id = $1
    AND profile_ref = $2
    AND binding_scope = $3
    AND binding_target_id = $4
  LIMIT 1`,
orgID,
profileRef,
bindingScope,
bindingTargetID,
).Scan(&workspaceRef)
if err == nil {
return workspaceRef, true, nil
}
if err == pgx.ErrNoRows {
return "", false, nil
}
return "", false, err
}

func insertWorkspaceBinding(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ownerUserID *uuid.UUID, profileRef string, bindingScope string, bindingTargetID uuid.UUID, workspaceRef string) error {
_, err := tx.Exec(
ctx,
`INSERT INTO default_workspace_bindings (
profile_ref,
owner_user_id,
org_id,
binding_scope,
binding_target_id,
workspace_ref
 ) VALUES ($1, $2, $3, $4, $5, $6)
 ON CONFLICT (org_id, profile_ref, binding_scope, binding_target_id) DO NOTHING`,
profileRef,
ownerUserID,
orgID,
bindingScope,
bindingTargetID,
workspaceRef,
)
return err
}

func upsertWorkspaceRegistry(ctx context.Context, tx pgx.Tx, workspaceRef string, orgID uuid.UUID, ownerUserID *uuid.UUID, projectID *uuid.UUID) error {
_, err := tx.Exec(
ctx,
`INSERT INTO workspace_registries (
workspace_ref,
org_id,
owner_user_id,
project_id,
flush_state,
flush_retry_count,
last_used_at,
metadata_json
) VALUES ($1, $2, $3, $4, 'idle', 0, $5, '{}'::jsonb)
ON CONFLICT (workspace_ref) DO UPDATE SET
owner_user_id = COALESCE(EXCLUDED.owner_user_id, workspace_registries.owner_user_id),
project_id = COALESCE(EXCLUDED.project_id, workspace_registries.project_id),
last_used_at = EXCLUDED.last_used_at,
updated_at = now()`,
workspaceRef,
orgID,
ownerUserID,
projectID,
time.Now().UTC(),
)
return err
}

func loadProfileDefaultWorkspace(ctx context.Context, tx pgx.Tx, profileRef string) (string, error) {
var workspaceRef *string
err := tx.QueryRow(ctx, `SELECT default_workspace_ref FROM profile_registries WHERE profile_ref = $1`, profileRef).Scan(&workspaceRef)
if err != nil {
if err == pgx.ErrNoRows {
return "", nil
}
return "", err
}
return ptrString(workspaceRef), nil
}

func touchProfileDefaultWorkspace(ctx context.Context, tx pgx.Tx, profileRef string, workspaceRef string) error {
_, err := tx.Exec(
ctx,
`UPDATE profile_registries
    SET default_workspace_ref = $2,
        last_used_at = now(),
        updated_at = now()
  WHERE profile_ref = $1`,
profileRef,
workspaceRef,
)
return err
}

func copyWorkspaceSkills(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ownerUserID uuid.UUID, sourceWorkspaceRef string, targetWorkspaceRef string) error {
if strings.TrimSpace(sourceWorkspaceRef) == "" || strings.TrimSpace(targetWorkspaceRef) == "" || sourceWorkspaceRef == targetWorkspaceRef {
return nil
}
_, err := tx.Exec(
ctx,
`INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version)
 SELECT $1, org_id, $2, skill_key, version
   FROM workspace_skill_enablements
  WHERE workspace_ref = $3 AND org_id = $4
 ON CONFLICT (workspace_ref, skill_key, version) DO NOTHING`,
targetWorkspaceRef,
ownerUserID,
sourceWorkspaceRef,
orgID,
)
return err
}

func syncWorkspaceMetadata(ctx context.Context, tx pgx.Tx, workspaceRef string) error {
rows, err := tx.Query(
ctx,
`SELECT skill_key, version
   FROM workspace_skill_enablements
  WHERE workspace_ref = $1
  ORDER BY skill_key, version`,
workspaceRef,
)
if err != nil {
return err
}
defer rows.Close()

refs := make([]string, 0)
for rows.Next() {
var skillKey string
var version string
if err := rows.Scan(&skillKey, &version); err != nil {
return err
}
refs = append(refs, strings.TrimSpace(skillKey)+"@"+strings.TrimSpace(version))
}
if err := rows.Err(); err != nil {
return err
}
sort.Strings(refs)
payload, err := json.Marshal(dedupeStrings(refs))
if err != nil {
return err
}
_, err = tx.Exec(
ctx,
`UPDATE workspace_registries
    SET metadata_json = jsonb_set(COALESCE(metadata_json, '{}'::jsonb), '{enabled_skill_refs}', $2::jsonb, true),
        updated_at = now()
  WHERE workspace_ref = $1`,
workspaceRef,
string(payload),
)
return err
}

func dedupeStrings(values []string) []string {
out := make([]string, 0, len(values))
seen := map[string]struct{}{}
for _, value := range values {
if _, ok := seen[value]; ok {
continue
}
seen[value] = struct{}{}
out = append(out, value)
}
return out
}

func profileRefForUser(userID uuid.UUID) string {
return "pref_" + strings.ReplaceAll(userID.String(), "-", "")
}

func workspaceRefForBinding(profileRef string, bindingScope string, bindingTargetID uuid.UUID) string {
prefix := "wsref_thread_"
if bindingScope == data.BindingScopeProject {
prefix = "wsref_project_"
}
return prefix + strings.ReplaceAll(profileRef, "pref_", "") + "_" + strings.ReplaceAll(bindingTargetID.String(), "-", "")
}

func stringPtr(value string) *string {
if strings.TrimSpace(value) == "" {
return nil
}
return &value
}

func ptrString(value *string) string {
if value == nil {
return ""
}
return *value
}
