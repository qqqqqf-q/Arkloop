//go:build desktop

package local

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// db is the minimal interface needed by the local memory provider.
type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// Provider implements memory.MemoryProvider using a local SQLite database.
// All storage is text-based — no vectors or embeddings are used.
type Provider struct {
	db db
}

// NewProvider creates a new local SQLite-backed memory provider.
func NewProvider(d db) *Provider {
	return &Provider{db: d}
}

// uriToID converts a local://memory/{id} URI back to the raw ID.
func uriToID(uri string) (string, error) {
	const prefix = "local://memory/"
	if !strings.HasPrefix(uri, prefix) {
		return "", fmt.Errorf("invalid local memory URI: %q", uri)
	}
	id := strings.TrimPrefix(uri, prefix)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("empty id in URI: %q", uri)
	}
	return id, nil
}

// idToURI builds a local://memory/{id} URI.
func idToURI(id string) string {
	return "local://memory/" + id
}

// Find performs a simple text search across memory entries for a given user.
// Returns up to limit hits ordered by most-recently created first.
func (p *Provider) Find(ctx context.Context, ident memory.MemoryIdentity, _ string, query string, limit int) ([]memory.MemoryHit, error) {
	if limit <= 0 {
		limit = 10
	}

	pattern := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"

	rows, err := p.db.Query(ctx,
		`SELECT id, scope, category, entry_key, content
		 FROM desktop_memory_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		   AND (content LIKE $4 OR category LIKE $4 OR entry_key LIKE $4)
		 ORDER BY created_at DESC
		 LIMIT $5`,
		ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
		pattern, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("memory find query: %w", err)
	}
	defer rows.Close()

	var hits []memory.MemoryHit
	for rows.Next() {
		var id, sc, cat, key, content string
		if err := rows.Scan(&id, &sc, &cat, &key, &content); err != nil {
			return nil, fmt.Errorf("memory find scan: %w", err)
		}
		abstract := buildAbstract(sc, cat, key, content)
		hits = append(hits, memory.MemoryHit{
			URI:      idToURI(id),
			Abstract: abstract,
			Score:    1.0,
			IsLeaf:   true,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory find rows: %w", err)
	}
	return hits, nil
}

// Content returns the full content of a memory entry by URI.
// The depth layer is ignored since all entries are single-level text.
func (p *Provider) Content(ctx context.Context, ident memory.MemoryIdentity, uri string, _ memory.MemoryLayer) (string, error) {
	id, err := uriToID(uri)
	if err != nil {
		return "", err
	}

	var sc, cat, key, content string
	err = p.db.QueryRow(ctx,
		`SELECT scope, category, entry_key, content
		 FROM desktop_memory_entries
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
	).Scan(&sc, &cat, &key, &content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("memory entry not found: %s", uri)
		}
		return "", fmt.Errorf("memory content query: %w", err)
	}
	return buildAbstract(sc, cat, key, content), nil
}

// AppendSessionMessages is a no-op for the local provider; session-level
// message archiving is not used in the lightweight local memory mode.
func (p *Provider) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	return nil
}

// CommitSession is a no-op for the local provider.
func (p *Provider) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

// Write inserts a new notebook entry.
func (p *Provider) Write(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, entry memory.MemoryEntry) error {
	_, err := p.WriteReturningURI(ctx, ident, scope, entry)
	return err
}

// WriteReturningURI inserts one row and returns local://memory/{id} for memory_read / memory_forget.
func (p *Provider) WriteReturningURI(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, entry memory.MemoryEntry) (string, error) {
	sc, cat, key := parseWritableContent(entry.Content)
	body := stripWritableHeader(entry.Content)
	if sc == "" {
		sc = string(scope)
	}

	var id string
	err := p.db.QueryRow(ctx,
		`INSERT INTO desktop_memory_entries (account_id, user_id, agent_id, scope, category, entry_key, content)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
		sc, cat, key, body,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("memory write insert: %w", err)
	}
	if err := p.rebuildSnapshot(ctx, ident); err != nil {
		return "", err
	}

	return idToURI(id), nil
}

// UpdateByURI overwrites an existing notebook entry and rebuilds the snapshot.
func (p *Provider) UpdateByURI(ctx context.Context, ident memory.MemoryIdentity, uri string, entry memory.MemoryEntry) error {
	id, err := uriToID(uri)
	if err != nil {
		return err
	}

	sc, cat, key := parseWritableContent(entry.Content)
	body := stripWritableHeader(entry.Content)
	if sc == "" {
		sc = string(memory.MemoryScopeUser)
	}

	tag, err := p.db.Exec(ctx,
		`UPDATE desktop_memory_entries
		 SET scope = $5, category = $6, entry_key = $7, content = $8
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
		sc, cat, key, body,
	)
	if err != nil {
		return fmt.Errorf("memory update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory entry not found: %s", uri)
	}

	return p.rebuildSnapshot(ctx, ident)
}

// Delete removes a notebook entry by URI.
func (p *Provider) Delete(ctx context.Context, ident memory.MemoryIdentity, uri string) error {
	id, err := uriToID(uri)
	if err != nil {
		return err
	}

	tag, err := p.db.Exec(ctx,
		`DELETE FROM desktop_memory_entries
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
	)
	if err != nil {
		return fmt.Errorf("memory delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory entry not found: %s", uri)
	}

	return p.rebuildSnapshot(ctx, ident)
}

// rebuildSnapshot reconstructs user_notebook_snapshots.notebook_block from all
// current entries so the notebook injection stays up to date.
func (p *Provider) rebuildSnapshot(ctx context.Context, ident memory.MemoryIdentity) error {
	rows, err := p.db.Query(ctx,
		`SELECT scope, category, entry_key, content
		 FROM desktop_memory_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		 ORDER BY created_at ASC`,
		ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID),
	)
	if err != nil {
		return fmt.Errorf("memory rebuild query: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var sc, cat, key, content string
		if err := rows.Scan(&sc, &cat, &key, &content); err != nil {
			return fmt.Errorf("memory rebuild scan: %w", err)
		}
		lines = append(lines, buildAbstract(sc, cat, key, content))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("memory rebuild rows: %w", err)
	}

	block := buildNotebookBlock(lines)

	_, err = p.db.Exec(ctx,
		`INSERT INTO user_notebook_snapshots (account_id, user_id, agent_id, notebook_block, updated_at)
		 VALUES ($1, $2, $3, $4, datetime('now'))
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET notebook_block = EXCLUDED.notebook_block, updated_at = EXCLUDED.updated_at`,
		ident.AccountID.String(), ident.UserID.String(), agentID(ident.AgentID), block,
	)
	if err != nil {
		return fmt.Errorf("notebook snapshot upsert: %w", err)
	}
	return nil
}

// Entry is the externally visible form of a single memory record (used by
// the API handler for the settings UI).
type Entry struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Category  string `json:"category"`
	Key       string `json:"key"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// List returns all memory entries for a user, most-recently created first.
// Intended for the settings UI, not the tool executor.
func (p *Provider) List(ctx context.Context, accountID, userID uuid.UUID, agentIDStr string) ([]Entry, error) {
	rows, err := p.db.Query(ctx,
		`SELECT id, scope, category, entry_key, content, created_at
		 FROM desktop_memory_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		 ORDER BY created_at DESC`,
		accountID.String(), userID.String(), agentID(agentIDStr),
	)
	if err != nil {
		return nil, fmt.Errorf("memory list: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Scope, &e.Category, &e.Key, &e.Content, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("memory list scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory list rows: %w", err)
	}
	return entries, nil
}

// DeleteByID removes a memory entry by raw ID (no URI prefix), rebuilding the
// snapshot afterwards. Intended for direct API calls from the settings UI.
func (p *Provider) DeleteByID(ctx context.Context, accountID, userID uuid.UUID, agentIDStr, id string) error {
	ident := memory.MemoryIdentity{
		AccountID: accountID,
		UserID:    userID,
		AgentID:   agentIDStr,
	}
	return p.Delete(ctx, ident, idToURI(id))
}

// GetSnapshot returns the raw notebook block currently stored in
// user_notebook_snapshots, used by the settings UI for display.
func (p *Provider) GetSnapshot(ctx context.Context, accountID, userID uuid.UUID, agentIDStr string) (string, error) {
	var block string
	err := p.db.QueryRow(ctx,
		`SELECT notebook_block FROM user_notebook_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID.String(), userID.String(), agentID(agentIDStr),
	).Scan(&block)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("notebook snapshot get: %w", err)
	}
	return block, nil
}

// ---------- helpers ----------

func agentID(id string) string {
	if strings.TrimSpace(id) == "" {
		return "default"
	}
	return id
}

// scopeFilter returns the scope value to filter on, or empty string for all scopes.
func scopeFilter(scope memory.MemoryScope) string {
	switch scope {
	case memory.MemoryScopeUser, memory.MemoryScopeAgent:
		return string(scope)
	}
	return ""
}

// buildAbstract formats a single display line for a memory entry.
func buildAbstract(scope, category, key, content string) string {
	if key != "" {
		return fmt.Sprintf("[%s/%s/%s] %s", scope, category, key, content)
	}
	return fmt.Sprintf("[%s/%s] %s", scope, category, content)
}

// parseWritableContent extracts scope/category/key from the formatted content
// string produced by executor.go's buildWritableContent().
// Format: "[scope/category/key] text" or just plain text.
func parseWritableContent(raw string) (scope, category, key string) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") {
		return "", "general", ""
	}
	end := strings.Index(raw, "]")
	if end < 0 {
		return "", "general", ""
	}
	header := raw[1:end]
	parts := strings.SplitN(header, "/", 3)
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], parts[1], ""
	default:
		return "", parts[0], ""
	}
}

func stripWritableHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") {
		return raw
	}
	end := strings.Index(raw, "]")
	if end < 0 {
		return raw
	}
	return strings.TrimSpace(raw[end+1:])
}

// buildNotebookBlock formats all entries into the XML notebook block used by the
// system prompt injection.
func buildNotebookBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<notebook>\n")
	for _, l := range lines {
		sb.WriteString("- ")
		sb.WriteString(strings.TrimSpace(l))
		sb.WriteString("\n")
	}
	sb.WriteString("</notebook>")
	return sb.String()
}
