//go:build !desktop

package notebook

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Provider implements notebook CRUD backed by PostgreSQL.
// Entries are stored in notebook_entries; the aggregated snapshot
// is cached in user_notebook_snapshots for system prompt injection.
type Provider struct {
	pool *pgxpool.Pool
}

func NewProvider(pool *pgxpool.Pool) *Provider {
	return &Provider{pool: pool}
}

func uriToID(uri string) (string, error) {
	const prefix = "local://memory/"
	if !strings.HasPrefix(uri, prefix) {
		return "", fmt.Errorf("invalid notebook URI: %q", uri)
	}
	id := strings.TrimPrefix(uri, prefix)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("empty id in URI: %q", uri)
	}
	return id, nil
}

func idToURI(id string) string {
	return "local://memory/" + id
}

// Find performs a text search across notebook entries.
func (p *Provider) Find(ctx context.Context, ident memory.MemoryIdentity, _ string, query string, limit int) ([]memory.MemoryHit, error) {
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"

	rows, err := p.pool.Query(ctx,
		`SELECT id, scope, category, entry_key, content
		 FROM notebook_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		   AND (content LIKE $4 OR category LIKE $4 OR entry_key LIKE $4)
		 ORDER BY created_at DESC
		 LIMIT $5`,
		ident.AccountID, ident.UserID, agentID(ident.AgentID),
		pattern, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("notebook find: %w", err)
	}
	defer rows.Close()

	var hits []memory.MemoryHit
	for rows.Next() {
		var id uuid.UUID
		var sc, cat, key, content string
		if err := rows.Scan(&id, &sc, &cat, &key, &content); err != nil {
			return nil, fmt.Errorf("notebook find scan: %w", err)
		}
		hits = append(hits, memory.MemoryHit{
			URI:      idToURI(id.String()),
			Abstract: buildAbstract(sc, cat, key, content),
			Score:    1.0,
			IsLeaf:   true,
		})
	}
	return hits, rows.Err()
}

// Content returns the full content of a notebook entry by URI.
func (p *Provider) Content(ctx context.Context, ident memory.MemoryIdentity, uri string, _ memory.MemoryLayer) (string, error) {
	id, err := uriToID(uri)
	if err != nil {
		return "", err
	}
	var sc, cat, key, content string
	err = p.pool.QueryRow(ctx,
		`SELECT scope, category, entry_key, content
		 FROM notebook_entries
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID, ident.UserID, agentID(ident.AgentID),
	).Scan(&sc, &cat, &key, &content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("notebook entry not found: %s", uri)
		}
		return "", fmt.Errorf("notebook content: %w", err)
	}
	return buildAbstract(sc, cat, key, content), nil
}

func (p *Provider) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	return nil
}

func (p *Provider) ListDir(_ context.Context, _ memory.MemoryIdentity, _ string) ([]string, error) {
	return nil, nil
}

func (p *Provider) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

func (p *Provider) Write(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, entry memory.MemoryEntry) error {
	_, err := p.WriteReturningURI(ctx, ident, scope, entry)
	return err
}

// WriteReturningURI inserts a notebook entry and returns its URI.
func (p *Provider) WriteReturningURI(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, entry memory.MemoryEntry) (string, error) {
	sc, cat, key := parseWritableContent(entry.Content)
	body := stripWritableHeader(entry.Content)
	if sc == "" {
		sc = string(scope)
	}

	var id uuid.UUID
	err := p.pool.QueryRow(ctx,
		`INSERT INTO notebook_entries (account_id, user_id, agent_id, scope, category, entry_key, content)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		ident.AccountID, ident.UserID, agentID(ident.AgentID),
		sc, cat, key, body,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("notebook write: %w", err)
	}
	if err := p.rebuildSnapshot(ctx, ident); err != nil {
		return "", err
	}
	return idToURI(id.String()), nil
}

// UpdateByURI overwrites an existing notebook entry.
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

	tag, err := p.pool.Exec(ctx,
		`UPDATE notebook_entries
		 SET scope = $5, category = $6, entry_key = $7, content = $8
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID, ident.UserID, agentID(ident.AgentID),
		sc, cat, key, body,
	)
	if err != nil {
		return fmt.Errorf("notebook update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notebook entry not found: %s", uri)
	}
	return p.rebuildSnapshot(ctx, ident)
}

// Delete removes a notebook entry by URI.
func (p *Provider) Delete(ctx context.Context, ident memory.MemoryIdentity, uri string) error {
	id, err := uriToID(uri)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM notebook_entries
		 WHERE id = $1 AND account_id = $2 AND user_id = $3 AND agent_id = $4`,
		id, ident.AccountID, ident.UserID, agentID(ident.AgentID),
	)
	if err != nil {
		return fmt.Errorf("notebook delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notebook entry not found: %s", uri)
	}
	return p.rebuildSnapshot(ctx, ident)
}

// GetSnapshot returns the cached notebook block for system prompt injection.
func (p *Provider) GetSnapshot(ctx context.Context, accountID, userID uuid.UUID, agentIDStr string) (string, error) {
	var block string
	err := p.pool.QueryRow(ctx,
		`SELECT notebook_block FROM user_notebook_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID(agentIDStr),
	).Scan(&block)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("notebook snapshot get: %w", err)
	}
	return block, nil
}

func (p *Provider) rebuildSnapshot(ctx context.Context, ident memory.MemoryIdentity) error {
	rows, err := p.pool.Query(ctx,
		`SELECT id, scope, category, entry_key, content
		 FROM notebook_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		 ORDER BY created_at ASC`,
		ident.AccountID, ident.UserID, agentID(ident.AgentID),
	)
	if err != nil {
		return fmt.Errorf("notebook rebuild: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var id uuid.UUID
		var sc, cat, key, content string
		if err := rows.Scan(&id, &sc, &cat, &key, &content); err != nil {
			return fmt.Errorf("notebook rebuild scan: %w", err)
		}
		lines = append(lines, buildSnapshotLine(id.String(), sc, cat, key, content))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("notebook rebuild rows: %w", err)
	}

	block := BuildNotebookBlock(lines)
	_, err = p.pool.Exec(ctx,
		`INSERT INTO user_notebook_snapshots (account_id, user_id, agent_id, notebook_block, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET notebook_block = EXCLUDED.notebook_block, updated_at = EXCLUDED.updated_at`,
		ident.AccountID, ident.UserID, agentID(ident.AgentID), block,
	)
	if err != nil {
		return fmt.Errorf("notebook snapshot upsert: %w", err)
	}
	return nil
}

// -- helpers --

func agentID(id string) string {
	if strings.TrimSpace(id) == "" {
		return "default"
	}
	return id
}

func buildAbstract(scope, category, key, content string) string {
	if key != "" {
		return fmt.Sprintf("[%s/%s/%s] %s", scope, category, key, content)
	}
	return fmt.Sprintf("[%s/%s] %s", scope, category, content)
}

func buildSnapshotLine(id, scope, category, key, content string) string {
	uri := idToURI(id)
	return fmt.Sprintf("(%s) %s", uri, buildAbstract(scope, category, key, content))
}

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

// BuildNotebookBlock formats entries into the <notebook> XML block.
// Each line is sanitized to prevent XML boundary escape attacks.
func BuildNotebookBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<notebook>\n")
	for _, l := range lines {
		sb.WriteString("- ")
		sb.WriteString(memory.EscapeXMLContent(strings.TrimSpace(l)))
		sb.WriteString("\n")
	}
	sb.WriteString("</notebook>")
	return sb.String()
}
