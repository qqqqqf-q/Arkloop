//go:build desktop

// Package memoryapi provides HTTP endpoints for the Desktop settings UI to
// inspect and manage notebook entries. These routes are desktop-only but still
// authenticate with the normal owner session.
package memoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/http/httpkit"
	"arkloop/services/shared/desktop"

	"github.com/google/uuid"
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
	Pool                     data.DB
	AuthService              *auth.Service
	MemoryProvider           string
	OpenVikingBaseURL        string
	OpenVikingAPIKey         string
	NowledgeBaseURL          string
	NowledgeAPIKey           string
	NowledgeRequestTimeoutMs int
}

// RegisterRoutes registers notebook management routes onto mux.
func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	h := &handler{
		pool:                     deps.Pool,
		authService:              deps.AuthService,
		memoryProvider:           strings.TrimSpace(deps.MemoryProvider),
		ovBaseURL:                deps.OpenVikingBaseURL,
		ovAPIKey:                 deps.OpenVikingAPIKey,
		nowledgeBaseURL:          deps.NowledgeBaseURL,
		nowledgeAPIKey:           deps.NowledgeAPIKey,
		nowledgeRequestTimeoutMs: deps.NowledgeRequestTimeoutMs,
	}
	mux.HandleFunc("/v1/desktop/memory/entries", h.dispatchEntries)
	mux.HandleFunc("/v1/desktop/memory/entries/", h.dispatchEntryByID)
	mux.HandleFunc("/v1/desktop/memory/status", h.getStatus)
	mux.HandleFunc("/v1/desktop/memory/snapshot", h.getSnapshot)
	mux.HandleFunc("/v1/desktop/memory/snapshot/rebuild", h.rebuildSnapshotHandler)
	mux.HandleFunc("/v1/desktop/memory/content", h.getContent)
	mux.HandleFunc("/v1/desktop/memory/impression", h.getImpression)
	mux.HandleFunc("/v1/desktop/memory/impression/rebuild", h.rebuildImpression)
}

type handler struct {
	pool                     data.DB
	authService              *auth.Service
	memoryProvider           string
	ovBaseURL                string
	ovAPIKey                 string
	nowledgeBaseURL          string
	nowledgeAPIKey           string
	nowledgeRequestTimeoutMs int
}

type memoryRuntimeStatus struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	Healthy    bool   `json:"healthy"`
	CheckedAt  string `json:"checked_at"`
	Error      string `json:"error,omitempty"`
	Details    struct {
		Nowledge struct {
			Version  string `json:"version,omitempty"`
			SearchOK *bool  `json:"search_ok,omitempty"`
		} `json:"nowledge,omitempty"`
		OpenViking struct {
			HealthOK *bool `json:"health_ok,omitempty"`
		} `json:"openviking,omitempty"`
	} `json:"details,omitempty"`
}

var impressionRebuildWaitTimeout = 90 * time.Second
var impressionRebuildPollInterval = 300 * time.Millisecond

type impressionState struct {
	impression string
	updatedAt  string
	found      bool
}

type nowledgeConfig struct {
	baseURL        string
	apiKey         string
	requestTimeout time.Duration
}

type nowledgeListedMemory struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Rating     float64  `json:"rating"`
	Time       string   `json:"time"`
	LabelIDs   []string `json:"label_ids"`
	Confidence float64  `json:"confidence"`
}

const (
	nowledgeMemoryURIPrefix  = "nowledge://memory/"
	nowledgeDefaultBaseURL   = "http://127.0.0.1:14242"
	nowledgeLocalConfigPath  = ".nowledge-mem/config.json"
	nowledgeSnapshotLimit    = 0
	nowledgeContentTimeoutMs = 30000
)

func (h *handler) dispatchEntries(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !h.checkAccessToken(w, r) {
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
	if !h.checkAccessToken(w, r) {
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

func (h *handler) getStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !h.checkAccessToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodGet {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}

	status := memoryRuntimeStatus{
		Provider:   h.activeMemoryProvider(),
		Configured: false,
		Healthy:    false,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	ctx := r.Context()
	agentID := agentIDFromQuery(r)

	switch status.Provider {
	case "nowledge":
		cfg, err := h.resolveNowledgeConfig()
		if err != nil {
			status.Error = err.Error()
			writeJSON(w, status)
			return
		}
		status.Configured = strings.TrimSpace(cfg.baseURL) != ""
		if !status.Configured {
			status.Error = "nowledge not configured"
			writeJSON(w, status)
			return
		}

		version, healthErr := h.checkNowledgeHealth(ctx, agentID, cfg)
		if strings.TrimSpace(version) != "" {
			status.Details.Nowledge.Version = version
		}

		searchOK, searchErr := h.checkNowledgeSearch(ctx, agentID, cfg)
		status.Details.Nowledge.SearchOK = boolPtr(searchOK)

		status.Healthy = healthErr == nil && searchErr == nil
		if !status.Healthy {
			// Keep error concise; UI should map this to generic labels.
			status.Error = firstNonEmpty(errString(healthErr), errString(searchErr))
		}

	case "openviking":
		status.Configured = strings.TrimSpace(h.ovBaseURL) != ""
		if !status.Configured {
			status.Error = "openviking not configured"
			writeJSON(w, status)
			return
		}
		ok, err := h.checkOpenVikingHealth(ctx)
		status.Details.OpenViking.HealthOK = boolPtr(ok)
		status.Healthy = err == nil && ok
		if !status.Healthy && err != nil {
			status.Error = err.Error()
		}

	default:
		// notebook mode is treated as healthy, but this endpoint is mainly for semantic memory health.
		status.Configured = true
		status.Healthy = true
	}

	writeJSON(w, status)
}

func (h *handler) checkOpenVikingHealth(ctx context.Context) (bool, error) {
	base := strings.TrimRight(strings.TrimSpace(h.ovBaseURL), "/")
	if base == "" {
		return false, fmt.Errorf("openviking not configured")
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, base+"/health", nil)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(h.ovAPIKey) != "" {
		req.Header.Set("X-API-Key", strings.TrimSpace(h.ovAPIKey))
	}
	client := &nethttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, fmt.Errorf("openviking %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	io.Copy(io.Discard, resp.Body)
	return true, nil
}

func (h *handler) checkNowledgeHealth(ctx context.Context, agentID string, cfg nowledgeConfig) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("nowledge base url is empty")
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, base+"/health", nil)
	if err != nil {
		return "", err
	}
	h.setNowledgeHeaders(req, agentID, cfg.apiKey)
	client := &nethttp.Client{Timeout: cfg.requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("nowledge %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &payload) == nil {
		return strings.TrimSpace(payload.Version), nil
	}
	return "", nil
}

func (h *handler) checkNowledgeSearch(ctx context.Context, agentID string, cfg nowledgeConfig) (bool, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.baseURL), "/")
	if base == "" {
		return false, fmt.Errorf("nowledge base url is empty")
	}
	values := url.Values{}
	values.Set("query", "arkloop")
	values.Set("limit", "1")
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, base+"/memories/search?"+values.Encode(), nil)
	if err != nil {
		return false, err
	}
	h.setNowledgeHeaders(req, agentID, cfg.apiKey)
	client := &nethttp.Client{Timeout: cfg.requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("nowledge %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func boolPtr(v bool) *bool { return &v }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *handler) getSnapshot(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !h.checkAccessToken(w, r) {
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
	if !h.checkAccessToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodPost {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}
	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()
	agentID := agentIDFromQuery(r)

	block, hits, err := h.rebuildSnapshot(r.Context(), agentID, userID)
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

func (h *handler) rebuildSnapshot(ctx context.Context, agentID, userID string) (string, []snapshotHit, error) {
	switch h.activeMemoryProvider() {
	case "nowledge":
		return h.findAndBuildNowledgeMemoryBlock(ctx, agentID)
	case "openviking":
		if strings.TrimSpace(h.ovBaseURL) == "" {
			return "", nil, fmt.Errorf("openviking not configured")
		}
		return h.findAndBuildMemoryBlock(ctx, userID)
	default:
		return "", nil, fmt.Errorf("memory provider does not support snapshot rebuild")
	}
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
	if !h.checkAccessToken(w, r) {
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
	content, err := h.fetchContent(r.Context(), agentIDFromQuery(r), uri, layer)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "upstream_error", err.Error(), "", nil)
		return
	}
	writeJSON(w, map[string]any{"content": content})
}

func (h *handler) fetchContent(ctx context.Context, agentID, uri, layer string) (string, error) {
	switch {
	case strings.HasPrefix(strings.TrimSpace(uri), nowledgeMemoryURIPrefix):
		return h.fetchNowledgeContent(ctx, agentID, uri, layer)
	default:
		if strings.TrimSpace(h.ovBaseURL) == "" {
			return "", fmt.Errorf("openviking not configured")
		}
		return h.fetchOVContent(ctx, uri, layer)
	}
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

func (h *handler) activeMemoryProvider() string {
	switch provider := strings.TrimSpace(h.memoryProvider); provider {
	case "nowledge", "openviking":
		return provider
	}
	if strings.TrimSpace(h.nowledgeBaseURL) != "" {
		return "nowledge"
	}
	if strings.TrimSpace(h.ovBaseURL) != "" {
		return "openviking"
	}
	return "notebook"
}

func (h *handler) resolveNowledgeConfig() (nowledgeConfig, error) {
	cfg := nowledgeConfig{
		baseURL: strings.TrimSpace(h.nowledgeBaseURL),
		apiKey:  strings.TrimSpace(h.nowledgeAPIKey),
	}
	if h.nowledgeRequestTimeoutMs > 0 {
		cfg.requestTimeout = time.Duration(h.nowledgeRequestTimeoutMs) * time.Millisecond
	} else {
		cfg.requestTimeout = nowledgeContentTimeoutMs * time.Millisecond
	}

	if cfg.baseURL == "" || cfg.apiKey == "" {
		localCfg, err := loadNowledgeLocalConfigFile()
		if err == nil {
			if cfg.baseURL == "" {
				cfg.baseURL = localCfg.baseURL
			}
			if cfg.apiKey == "" {
				cfg.apiKey = localCfg.apiKey
			}
		}
	}
	if cfg.baseURL == "" {
		cfg.baseURL = nowledgeDefaultBaseURL
	}
	if cfg.requestTimeout <= 0 {
		cfg.requestTimeout = nowledgeContentTimeoutMs * time.Millisecond
	}
	return cfg, nil
}

func loadNowledgeLocalConfigFile() (nowledgeConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nowledgeConfig{}, err
	}
	raw, err := os.ReadFile(filepath.Join(homeDir, nowledgeLocalConfigPath))
	if err != nil {
		return nowledgeConfig{}, err
	}
	var payload struct {
		APIURL string `json:"apiUrl"`
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nowledgeConfig{}, err
	}
	return nowledgeConfig{
		baseURL: strings.TrimSpace(payload.APIURL),
		apiKey:  strings.TrimSpace(payload.APIKey),
	}, nil
}

func (h *handler) findAndBuildNowledgeMemoryBlock(ctx context.Context, agentID string) (string, []snapshotHit, error) {
	memories, err := h.listNowledgeMemories(ctx, agentID, nowledgeSnapshotLimit)
	if err != nil {
		return "", nil, err
	}
	return buildNowledgeSnapshotBlock(memories), buildNowledgeSnapshotHits(memories), nil
}

func (h *handler) listNowledgeMemories(ctx context.Context, agentID string, limit int) ([]nowledgeListedMemory, error) {
	const maxPageSize = 100

	cfg, err := h.resolveNowledgeConfig()
	if err != nil {
		return nil, err
	}
	target := limit
	if target < 0 {
		target = 0
	}
	if target <= 0 {
		target = nowledgeSnapshotLimit
	}

	client := &nethttp.Client{Timeout: cfg.requestTimeout}
	offset := 0
	out := make([]nowledgeListedMemory, 0, minInt(target, maxPageSize))
	for {
		pageSize := maxPageSize
		if target > 0 {
			remaining := target - len(out)
			if remaining <= 0 {
				break
			}
			pageSize = minInt(remaining, maxPageSize)
		}

		values := url.Values{}
		values.Set("limit", fmt.Sprintf("%d", pageSize))
		if offset > 0 {
			values.Set("offset", fmt.Sprintf("%d", offset))
		}
		path := strings.TrimRight(cfg.baseURL, "/") + "/memories?" + values.Encode()
		req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		h.setNowledgeHeaders(req, agentID, cfg.apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("nowledge list memories: %w", err)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read nowledge memories: %w", readErr)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("nowledge %d: %s", resp.StatusCode, string(body))
		}

		var wrapper struct {
			Memories   []nowledgeListedMemory `json:"memories"`
			Pagination struct {
				Total   int  `json:"total"`
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, fmt.Errorf("decode nowledge memories: %w", err)
		}

		out = append(out, wrapper.Memories...)
		offset += len(wrapper.Memories)
		if len(wrapper.Memories) == 0 {
			break
		}
		if target > 0 && len(out) >= target {
			break
		}
		if !wrapper.Pagination.HasMore {
			if wrapper.Pagination.Total <= 0 || offset >= wrapper.Pagination.Total {
				break
			}
		}
	}
	return out, nil
}

func minInt(a, b int) int {
	if a <= 0 {
		return b
	}
	if a < b {
		return a
	}
	return b
}

func buildNowledgeSnapshotBlock(memories []nowledgeListedMemory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n<memory>\n")
	for _, item := range memories {
		line := formatNowledgeSnapshotLine(item)
		if line == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("</memory>")
	if strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(sb.String(), "<memory>", ""), "</memory>", "")) == "" {
		return ""
	}
	return sb.String()
}

func buildNowledgeSnapshotHits(memories []nowledgeListedMemory) []snapshotHit {
	hits := make([]snapshotHit, 0, len(memories))
	for _, item := range memories {
		uri := strings.TrimSpace(item.ID)
		if uri == "" {
			continue
		}
		hits = append(hits, snapshotHit{
			URI:      nowledgeMemoryURIPrefix + uri,
			Abstract: firstNonEmpty(strings.TrimSpace(item.Title), compactInline(item.Content, 160)),
			IsLeaf:   true,
		})
	}
	return hits
}

func formatNowledgeSnapshotLine(item nowledgeListedMemory) string {
	title := strings.TrimSpace(item.Title)
	content := compactInline(item.Content, 160)
	switch {
	case title != "" && content != "" && content != title:
		return "[" + title + "] " + content
	case title != "":
		return "[" + title + "]"
	default:
		return content
	}
}

func (h *handler) fetchNowledgeContent(ctx context.Context, agentID, uri, layer string) (string, error) {
	memoryID, err := nowledgeMemoryIDFromURI(uri)
	if err != nil {
		return "", err
	}
	cfg, err := h.resolveNowledgeConfig()
	if err != nil {
		return "", err
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, strings.TrimRight(cfg.baseURL, "/")+"/memories/"+url.PathEscape(memoryID), nil)
	if err != nil {
		return "", err
	}
	h.setNowledgeHeaders(req, agentID, cfg.apiKey)
	client := &nethttp.Client{Timeout: cfg.requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nowledge content: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("read nowledge content: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("nowledge %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode nowledge content: %w", err)
	}
	title := strings.TrimSpace(payload.Title)
	content := strings.TrimSpace(payload.Content)
	if title == "" {
		return content, nil
	}
	if layer == "overview" && content != "" {
		return title + "\n" + content, nil
	}
	if content == "" {
		return title, nil
	}
	return title + "\n\n" + content, nil
}

func (h *handler) setNowledgeHeaders(req *nethttp.Request, agentID, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
		req.Header.Set("x-nmem-api-key", strings.TrimSpace(apiKey))
	}
	req.Header.Set("X-Arkloop-Account", auth.DesktopAccountID.String())
	req.Header.Set("X-Arkloop-User", auth.DesktopUserID.String())
	req.Header.Set("X-Arkloop-Agent", strings.TrimSpace(agentID))
	req.Header.Set("X-Arkloop-App", "arkloop")
}

func nowledgeMemoryIDFromURI(uri string) (string, error) {
	value := strings.TrimSpace(uri)
	if !strings.HasPrefix(value, nowledgeMemoryURIPrefix) {
		return "", fmt.Errorf("invalid nowledge memory uri: %q", uri)
	}
	id := strings.TrimSpace(strings.TrimPrefix(value, nowledgeMemoryURIPrefix))
	if id == "" {
		return "", fmt.Errorf("invalid nowledge memory uri: %q", uri)
	}
	return id, nil
}

func compactInline(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// ---------- helpers ----------

func agentIDFromQuery(r *nethttp.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if id == "" {
		return "user_" + auth.DesktopUserID.String()
	}
	return id
}

func (h *handler) checkAccessToken(w nethttp.ResponseWriter, r *nethttp.Request) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)
	if h == nil || h.authService == nil || token == "" {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "invalid token", "", nil)
		return false
	}
	verified, err := h.authService.VerifyAccessTokenForActor(r.Context(), token)
	if err != nil || verified.UserID != auth.DesktopUserID || verified.AccountID != auth.DesktopAccountID {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", "invalid token", "", nil)
		return false
	}
	return true
}

func (h *handler) getImpression(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !h.checkAccessToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodGet {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}
	agentID := agentIDFromQuery(r)
	accountID := auth.DesktopAccountID.String()
	userID := auth.DesktopUserID.String()

	var impression, updatedAt string
	err := h.pool.QueryRow(r.Context(),
		`SELECT impression, updated_at FROM user_impression_snapshots WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID,
	).Scan(&impression, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, map[string]any{"impression": ""})
			return
		}
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	writeJSON(w, map[string]any{"impression": impression, "updated_at": updatedAt})
}

func getImpressionState(ctx context.Context, pool data.Querier, accountID, userID, agentID string) (impressionState, error) {
	var state impressionState
	err := pool.QueryRow(ctx,
		`SELECT impression, updated_at FROM user_impression_snapshots WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID,
	).Scan(&state.impression, &state.updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return impressionState{}, nil
		}
		return impressionState{}, err
	}
	state.found = true
	return state, nil
}

func getRunStatus(ctx context.Context, pool data.Querier, runID uuid.UUID) (string, error) {
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1`, runID).Scan(&status); err != nil {
		return "", err
	}
	return strings.TrimSpace(status), nil
}

func isRunTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "interrupted", "cancelled":
		return true
	default:
		return false
	}
}

func impressionUpdatedAfterRebuild(before, after impressionState) bool {
	if !after.found {
		return false
	}
	if !before.found {
		return true
	}
	return after.updatedAt != before.updatedAt || after.impression != before.impression
}

func (h *handler) waitForImpressionRebuild(ctx context.Context, runID uuid.UUID, accountID, userID, agentID string, before impressionState) (impressionState, error) {
	ticker := time.NewTicker(impressionRebuildPollInterval)
	defer ticker.Stop()

	for {
		status, err := getRunStatus(ctx, h.pool, runID)
		if err != nil {
			return impressionState{}, err
		}
		if isRunTerminalStatus(status) {
			if status != "completed" {
				return impressionState{}, fmt.Errorf("impression rebuild %s", status)
			}
			after, err := getImpressionState(ctx, h.pool, accountID, userID, agentID)
			if err != nil {
				return impressionState{}, err
			}
			if impressionUpdatedAfterRebuild(before, after) {
				return after, nil
			}
		}

		select {
		case <-ctx.Done():
			return impressionState{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (h *handler) rebuildImpression(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !h.checkAccessToken(w, r) {
		return
	}
	if r.Method != nethttp.MethodPost {
		httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
		return
	}

	accountID := auth.DesktopAccountID
	userID := auth.DesktopUserID
	agentID := agentIDFromQuery(r)
	accountIDText := accountID.String()
	userIDText := userID.String()

	enq := desktop.GetJobEnqueuer()
	if enq == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "unavailable", "job queue not ready", "", nil)
		return
	}

	ctx := r.Context()
	threadID := uuid.New()
	runID := uuid.New()
	traceID := uuid.NewString()
	before, err := getImpressionState(ctx, h.pool, accountIDText, userIDText, agentID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	var projectID string
	if err := h.pool.QueryRow(ctx,
		`SELECT id FROM projects WHERE account_id = $1 ORDER BY created_at ASC LIMIT 1`,
		accountIDText,
	).Scan(&projectID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	_ = agentID // agent_id 通过 pipeline 的 StableAgentID 自动推导

	if _, err := h.pool.Exec(ctx,
		`INSERT INTO threads (id, account_id, project_id, is_private) VALUES ($1, $2, $3, TRUE)`,
		threadID, accountIDText, projectID,
	); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	startedJSON, _ := json.Marshal(map[string]any{"run_kind": "impression", "persona_id": "impression-builder"})
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, status, created_by_user_id) VALUES ($1, $2, $3, 'running', $4)`,
		runID, accountIDText, threadID, userIDText,
	); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', $2)`,
		runID, string(startedJSON),
	); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	payload := map[string]any{
		"source":   "impression_rebuild",
		"run_kind": "impression",
	}
	if _, err := enq.EnqueueRun(ctx, accountID, runID, traceID, "run.execute", payload, nil); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		return
	}

	waitCtx, cancel := context.WithTimeout(ctx, impressionRebuildWaitTimeout)
	defer cancel()

	after, err := h.waitForImpressionRebuild(waitCtx, runID, accountIDText, userIDText, agentID, before)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			httpkit.WriteError(w, nethttp.StatusGatewayTimeout, "timeout", "impression rebuild timed out", "", nil)
		case errors.Is(err, context.Canceled):
			return
		default:
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal_error", err.Error(), "", nil)
		}
		return
	}

	writeJSON(w, map[string]any{
		"status":     "completed",
		"run_id":     runID.String(),
		"updated_at": after.updatedAt,
	})
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
