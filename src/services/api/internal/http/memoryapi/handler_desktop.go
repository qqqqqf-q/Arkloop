//go:build desktop

// Package memoryapi provides HTTP endpoints for the Desktop settings UI to
// inspect and manage notebook entries. These routes are desktop-only and use
// the fixed desktop token for authentication.
package memoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/http/httpkit"

	"github.com/jackc/pgx/v5"
)

// MemoryEntry is the JSON-serialisable form of a single desktop notebook record.
type MemoryEntry struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Category  string `json:"category"`
	Key       string `json:"key"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// Deps holds the dependencies for the notebook API.
type Deps struct {
	Pool data.DB
}

// RegisterRoutes registers notebook management routes onto mux.
func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	h := &handler{pool: deps.Pool}
	mux.HandleFunc("/v1/desktop/memory/entries", h.dispatchEntries)
	mux.HandleFunc("/v1/desktop/memory/entries/", h.dispatchEntryByID)
	mux.HandleFunc("/v1/desktop/memory/snapshot", h.getSnapshot)
}

type handler struct {
	pool data.DB
}

func (h *handler) dispatchEntries(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		h.listEntries(w, r)
	default:
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
	}
}

func (h *handler) dispatchEntryByID(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/desktop/memory/entries/")
	id = strings.TrimSpace(id)
	if id == "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "bad_request", "missing entry id", "", nil)
		return
	}
	switch r.Method {
	case nethttp.MethodDelete:
		h.deleteEntry(w, r, id)
	default:
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
	}
}

func (h *handler) getSnapshot(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodGet {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}
	agentID := agentIDFromQuery(r)
	block, err := getMemoryBlock(r.Context(), h.pool, auth.DesktopAccountID.String(), auth.DesktopUserID.String(), agentID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	writeJSON(w, map[string]any{"memory_block": block})
}

func (h *handler) listEntries(w nethttp.ResponseWriter, r *nethttp.Request) {
	agentID := agentIDFromQuery(r)
	entries, err := listMemoryEntries(r.Context(), h.pool, auth.DesktopAccountID.String(), auth.DesktopUserID.String(), agentID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	if entries == nil {
		entries = []MemoryEntry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}

func (h *handler) deleteEntry(w nethttp.ResponseWriter, r *nethttp.Request, id string) {
	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()

	// First fetch the entry's agent_id so the snapshot rebuild uses the right scope.
	var agentID string
	err := h.pool.QueryRow(r.Context(),
		`SELECT agent_id FROM desktop_memory_entries WHERE id = $1 AND account_id = $2 AND user_id = $3`,
		id, accountID, userID,
	).Scan(&agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", fmt.Sprintf("memory entry %s not found", id), "", nil)
		return
	}
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	tag, err := h.pool.Exec(r.Context(),
		`DELETE FROM desktop_memory_entries WHERE id = $1 AND account_id = $2 AND user_id = $3`,
		id, accountID, userID,
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		httpkit.WriteError(w, nethttp.StatusNotFound, "not_found", fmt.Sprintf("memory entry %s not found", id), "", nil)
		return
	}
	// Rebuild the memory_block snapshot for the affected agent.
	if err := rebuildMemoryBlock(r.Context(), h.pool, accountID, userID, agentID); err != nil {
		_ = err // Non-fatal; entry is already deleted.
	}
	writeJSON(w, map[string]any{"status": "ok"})
}

// ---------- queries ----------

// listMemoryEntries returns all notebook entries for a user across all agents.
// The settings UI shows a unified view regardless of which persona wrote each entry.
func listMemoryEntries(ctx context.Context, pool data.DB, accountID, userID, _ string) ([]MemoryEntry, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, scope, category, entry_key, content, created_at
		 FROM desktop_memory_entries
		 WHERE account_id = $1 AND user_id = $2
		 ORDER BY created_at DESC`,
		accountID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list memory entries: %w", err)
	}
	defer rows.Close()

	var entries []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := rows.Scan(&e.ID, &e.Scope, &e.Category, &e.Key, &e.Content, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan memory entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func getMemoryBlock(ctx context.Context, pool data.DB, accountID, userID, agentID string) (string, error) {
	var block string
	err := pool.QueryRow(ctx,
		`SELECT notebook_block FROM user_notebook_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID,
	).Scan(&block)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get memory block: %w", err)
	}
	return block, nil
}

func rebuildMemoryBlock(ctx context.Context, pool data.DB, accountID, userID, agentID string) error {
	rows, err := pool.Query(ctx,
		`SELECT scope, category, entry_key, content
		 FROM desktop_memory_entries
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		 ORDER BY created_at ASC`,
		accountID, userID, agentID,
	)
	if err != nil {
		return fmt.Errorf("rebuild memory block query: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var sc, cat, key, content string
		if err := rows.Scan(&sc, &cat, &key, &content); err != nil {
			return fmt.Errorf("rebuild scan: %w", err)
		}
		if key != "" {
			lines = append(lines, fmt.Sprintf("[%s/%s/%s] %s", sc, cat, key, content))
		} else {
			lines = append(lines, fmt.Sprintf("[%s/%s] %s", sc, cat, content))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	block := buildNotebookBlock(lines)
	_, err = pool.Exec(ctx,
		`INSERT INTO user_notebook_snapshots (account_id, user_id, agent_id, notebook_block, updated_at)
		 VALUES ($1, $2, $3, $4, datetime('now'))
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET notebook_block = EXCLUDED.notebook_block, updated_at = EXCLUDED.updated_at`,
		accountID, userID, agentID, block,
	)
	return err
}

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

// ---------- helpers ----------

func agentIDFromQuery(r *nethttp.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if id == "" {
		return "user_" + auth.DesktopUserID.String()
	}
	return id
}

func checkDesktopToken(w nethttp.ResponseWriter, r *nethttp.Request) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)
	if token != auth.DesktopToken() {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "invalid token", "", nil)
		return false
	}
	return true
}

func writeJSON(w nethttp.ResponseWriter, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", "marshal failed", "", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write(payload)
}
