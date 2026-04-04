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
	"io"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"

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
	Pool              data.DB
	OpenVikingBaseURL string
	OpenVikingAPIKey  string
}

// RegisterRoutes registers notebook management routes onto mux.
func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	h := &handler{pool: deps.Pool, ovBaseURL: deps.OpenVikingBaseURL, ovAPIKey: deps.OpenVikingAPIKey}
	mux.HandleFunc("/v1/desktop/memory/entries", h.dispatchEntries)
	mux.HandleFunc("/v1/desktop/memory/entries/", h.dispatchEntryByID)
	mux.HandleFunc("/v1/desktop/memory/snapshot", h.getSnapshot)
	mux.HandleFunc("/v1/desktop/memory/snapshot/rebuild", h.rebuildSnapshotHandler)
	mux.HandleFunc("/v1/desktop/memory/content", h.getContent)
}

type handler struct {
	pool      data.DB
	ovBaseURL string
	ovAPIKey  string
}

func (h *handler) dispatchEntries(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		h.listEntries(w, r)
	case nethttp.MethodPost:
		h.addEntry(w, r)
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
	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()
	block, err := getMemoryBlock(r.Context(), h.pool, accountID, userID, agentID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	hits, _ := getMemoryHits(r.Context(), h.pool, accountID, userID, agentID)
	writeJSON(w, map[string]any{"memory_block": block, "hits": hits})
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

func (h *handler) addEntry(w nethttp.ResponseWriter, r *nethttp.Request) {
	type addRequest struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "bad_request", "invalid json body", "", nil)
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "bad_request", "content is required", "", nil)
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}

	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()
	agentID := agentIDFromQuery(r)

	var entry MemoryEntry
	err := h.pool.QueryRow(r.Context(),
		`INSERT INTO desktop_memory_entries (account_id, user_id, agent_id, scope, category, entry_key, content)
		 VALUES ($1, $2, $3, 'user', $4, '', $5)
		 RETURNING id, scope, category, entry_key, content, created_at`,
		accountID, userID, agentID, req.Category, req.Content,
	).Scan(&entry.ID, &entry.Scope, &entry.Category, &entry.Key, &entry.Content, &entry.CreatedAt)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	if err := rebuildMemoryBlock(r.Context(), h.pool, accountID, userID, agentID); err != nil {
		_ = err // Non-fatal; entry is already inserted.
	}
	writeJSON(w, map[string]any{"entry": entry})
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

func (h *handler) rebuildSnapshotHandler(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodPost {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}
	if strings.TrimSpace(h.ovBaseURL) == "" {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "unavailable", "openviking not configured", "", nil)
		return
	}
	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()
	agentID := agentIDFromQuery(r)

	block, hits, err := h.findAndBuildMemoryBlock(r.Context(), userID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "upstream_error", err.Error(), "", nil)
		return
	}
	hitsJSON, _ := json.Marshal(hits)
	_, err = h.pool.Exec(r.Context(),
		`INSERT INTO user_memory_snapshots (account_id, user_id, agent_id, memory_block, hits_json, updated_at)
		 VALUES ($1, $2, $3, $4, $5, datetime('now'))
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET memory_block = EXCLUDED.memory_block, hits_json = EXCLUDED.hits_json, updated_at = EXCLUDED.updated_at`,
		accountID, userID, agentID, block, string(hitsJSON),
	)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	writeJSON(w, map[string]any{"memory_block": block, "hits": hits})
}

func (h *handler) findAndBuildMemoryBlock(ctx context.Context, userID string) (string, []snapshotHit, error) {
	rootURI := fmt.Sprintf("viking://user/%s/memories/", userID)

	var skeletonLines []string
	var leafLines []string
	var hits []snapshotHit

	rootOverview, err := h.fetchOVContent(ctx, rootURI, "overview")
	if err == nil && strings.TrimSpace(rootOverview) != "" {
		skeletonLines = append(skeletonLines, strings.TrimSpace(rootOverview))
	}

	children, err := h.fetchOVListDir(ctx, rootURI)
	if err != nil {
		if len(skeletonLines) > 0 {
			return buildTreeShapedBlock(skeletonLines, nil), hits, nil
		}
		return "", nil, fmt.Errorf("ls root: %w", err)
	}

	dirCount := 0
	for _, child := range children {
		if child.IsDir {
			if dirCount >= skeletonMaxDirs {
				continue
			}
			dirCount++
			childOverview, childErr := h.fetchOVContent(ctx, child.URI, "overview")
			if childErr == nil && strings.TrimSpace(childOverview) != "" {
				skeletonLines = append(skeletonLines, strings.TrimSpace(childOverview))
			}
			// hit 用 ls 返回的 L0 abstract，不用完整 overview
			abstract := strings.TrimSpace(child.Abstract)
			if abstract == "" {
				abstract = firstLine(strings.TrimSpace(childOverview))
			}
			if abstract != "" {
				hits = append(hits, snapshotHit{
					URI:      strings.TrimSuffix(child.URI, "/"),
					Abstract: abstract,
					IsLeaf:   false,
				})
			}
			subChildren, subErr := h.fetchOVListDir(ctx, child.URI)
			if subErr != nil {
				continue
			}
			leafCount := 0
			for _, sub := range subChildren {
				if leafCount >= leafMaxPerDir {
					break
				}
				if sub.IsDir {
					continue
				}
				content, readErr := h.fetchOVContent(ctx, sub.URI, "read")
				if readErr == nil && strings.TrimSpace(content) != "" {
					leafLines = append(leafLines, strings.TrimSpace(content))
					leafCount++
					hits = append(hits, snapshotHit{
						URI:      sub.URI,
						Abstract: firstLine(strings.TrimSpace(content)),
						IsLeaf:   true,
					})
				}
			}
		} else {
			content, readErr := h.fetchOVContent(ctx, child.URI, "read")
			if readErr == nil && strings.TrimSpace(content) != "" {
				leafLines = append(leafLines, strings.TrimSpace(content))
				hits = append(hits, snapshotHit{
					URI:      child.URI,
					Abstract: firstLine(strings.TrimSpace(content)),
					IsLeaf:   true,
				})
			}
		}
	}

	if len(skeletonLines) == 0 && len(leafLines) == 0 {
		return "", nil, nil
	}
	return buildTreeShapedBlock(skeletonLines, leafLines), hits, nil
}

const (
	skeletonMaxDirs = 10
	leafMaxPerDir   = 30
)

func (h *handler) fetchOVListDir(ctx context.Context, uri string) ([]lsEntry, error) {
	ovURL := fmt.Sprintf("%s/api/v1/fs/ls?uri=%s",
		strings.TrimRight(h.ovBaseURL, "/"), url.QueryEscape(uri))

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, ovURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.ovAPIKey != "" {
		req.Header.Set("X-API-Key", h.ovAPIKey)
	}
	req.Header.Set("X-OpenViking-Account", auth.DesktopAccountID.String())
	req.Header.Set("X-OpenViking-User", auth.DesktopUserID.String())
	req.Header.Set("X-OpenViking-Agent", "user_"+auth.DesktopUserID.String())

	client := &nethttp.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openviking ls: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read ls response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openviking %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("decode ls response: %w", err)
	}
	var rawEntries []lsEntry
	if err := json.Unmarshal(wrapper.Result, &rawEntries); err != nil {
		return nil, fmt.Errorf("decode ls result: %w", err)
	}
	for i := range rawEntries {
		if rawEntries[i].IsDir && !strings.HasSuffix(rawEntries[i].URI, "/") {
			rawEntries[i].URI += "/"
		}
	}
	return rawEntries, nil
}

func buildTreeShapedBlock(skeletonLines []string, leafLines []string) string {
	if len(skeletonLines) == 0 && len(leafLines) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, line := range skeletonLines {
		cleaned := strings.TrimSpace(line)
		if cleaned == "" {
			continue
		}
		sb.WriteString(cleaned)
		sb.WriteString("\n\n")
	}
	if len(leafLines) > 0 {
		if len(skeletonLines) > 0 {
			sb.WriteString("---\n")
		}
		for _, line := range leafLines {
			cleaned := strings.TrimSpace(line)
			if cleaned == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(cleaned)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</memory>")
	return sb.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	runes := []rune(s)
	if len(runes) > 100 {
		return string(runes[:100])
	}
	return s
}

// ---------- content ----------

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

type snapshotHit struct {
	URI      string `json:"uri"`
	Abstract string `json:"abstract"`
	IsLeaf   bool   `json:"is_leaf"`
}

type lsEntry struct {
	URI      string `json:"uri"`
	IsDir    bool   `json:"isDir"`
	Abstract string `json:"abstract"`
}

func getMemoryBlock(ctx context.Context, pool data.DB, accountID, userID, agentID string) (string, error) {
	var block string
	err := pool.QueryRow(ctx,
		`SELECT memory_block FROM user_memory_snapshots
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

func getMemoryHits(ctx context.Context, pool data.DB, accountID, userID, agentID string) ([]snapshotHit, error) {
	var raw []byte
	err := pool.QueryRow(ctx,
		`SELECT hits_json FROM user_memory_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3 AND hits_json IS NOT NULL`,
		accountID, userID, agentID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var hits []snapshotHit
	if err := json.Unmarshal(raw, &hits); err != nil {
		return nil, nil
	}
	return hits, nil
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

func (h *handler) getContent(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !checkDesktopToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodGet {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}
	uri := strings.TrimSpace(r.URL.Query().Get("uri"))
	layer := strings.TrimSpace(r.URL.Query().Get("layer"))
	if uri == "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "bad_request", "uri is required", "", nil)
		return
	}
	if layer == "" {
		layer = "overview"
	}
	if layer != "overview" && layer != "read" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "bad_request", "layer must be overview or read", "", nil)
		return
	}
	if strings.TrimSpace(h.ovBaseURL) == "" {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "unavailable", "openviking not configured", "", nil)
		return
	}

	content, err := h.fetchOVContent(r.Context(), uri, layer)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "upstream_error", err.Error(), "", nil)
		return
	}
	writeJSON(w, map[string]any{"content": content})
}

func (h *handler) fetchOVContent(ctx context.Context, uri, layer string) (string, error) {
	ovURL := fmt.Sprintf("%s/api/v1/content/%s?uri=%s",
		strings.TrimRight(h.ovBaseURL, "/"), url.PathEscape(layer), url.QueryEscape(uri))

	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, ovURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.ovAPIKey != "" {
		req.Header.Set("X-API-Key", h.ovAPIKey)
	}
	req.Header.Set("X-OpenViking-Account", auth.DesktopAccountID.String())
	req.Header.Set("X-OpenViking-User", auth.DesktopUserID.String())
	agentID := "user_" + auth.DesktopUserID.String()
	req.Header.Set("X-OpenViking-Agent", agentID)

	client := &nethttp.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openviking request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openviking %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return string(body), nil
	}
	var content string
	if err := json.Unmarshal(wrapper.Result, &content); err != nil {
		return string(wrapper.Result), nil
	}
	return content, nil
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
