package adminapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"strconv"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const accessLogStreamKey = "arkloop:gateway:access_log"

type accessLogEntry struct {
	ID           string `json:"id"`
	Timestamp    string `json:"timestamp"`
	TraceID      string `json:"trace_id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	StatusCode   int    `json:"status_code"`
	DurationMs   int64  `json:"duration_ms"`
	ClientIP     string `json:"client_ip"`
	Country      string `json:"country"`
	City         string `json:"city"`
	UserAgent    string `json:"user_agent"`
	UAType       string `json:"ua_type"`
	RiskScore    int    `json:"risk_score"`
	IdentityType string `json:"identity_type"`
	AccountID        string `json:"account_id"`
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
}

type accessLogFilters struct {
	method  string
	path    string
	ip      string
	country string
	riskMin int
	uaType  string
}

func adminAccessLogEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	usersRepo *data.UserRepository,
	rdb *redis.Client,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		listAccessLog(w, r, authService, membershipRepo, apiKeysRepo, usersRepo, rdb)
	}
}

func listAccessLog(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	usersRepo *data.UserRepository,
	rdb *redis.Client,
) {
	traceID := observability.TraceIDFromContext(r.Context())

	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
		return
	}

	if rdb == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "redis.not_configured", "redis not configured", traceID, nil)
		return
	}

	q := r.URL.Query()

	count := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "limit must be 1-200", traceID, nil)
			return
		}
		count = n
	}

	end := "+"
	if v := q.Get("before"); v != "" {
		end = v
	}
	start := "-"
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid since: must be RFC3339", traceID, nil)
			return
		}
		start = strconv.FormatInt(t.UnixMilli(), 10)
	}

	// 解析过滤条件
	filters := accessLogFilters{
		method:  strings.ToUpper(strings.TrimSpace(q.Get("method"))),
		path:    strings.TrimSpace(q.Get("path")),
		ip:      strings.TrimSpace(q.Get("ip")),
		country: strings.TrimSpace(q.Get("country")),
		uaType:  strings.TrimSpace(q.Get("ua_type")),
	}
	if v := q.Get("risk_min"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100 {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "risk_min must be 0-100", traceID, nil)
			return
		}
		filters.riskMin = n
	}

	hasFilters := filters.method != "" || filters.path != "" || filters.ip != "" ||
		filters.country != "" || filters.riskMin > 0 || filters.uaType != ""

	// 有过滤条件时需要扫描全量数据（stream MAXLEN = 10000）
	fetchSize := int64(count + 1)
	if hasFilters {
		fetchSize = 10001
	}

	messages, err := rdb.XRevRangeN(r.Context(), accessLogStreamKey, end, start, fetchSize).Result()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	entries := make([]accessLogEntry, 0, count)
	for _, msg := range messages {
		e := parseAccessLogMessage(msg.ID, msg.Values)
		if hasFilters && !matchFilters(e, filters) {
			continue
		}
		entries = append(entries, e)
		if len(entries) > count {
			break
		}
	}

	hasMore := len(entries) > count
	if hasMore {
		entries = entries[:count]
	}

	// 批量解析 user_id → username
	if usersRepo != nil {
		resolveUsernames(r.Context(), entries, usersRepo)
	}

	resp := map[string]any{
		"data":     entries,
		"has_more": hasMore,
	}
	if len(entries) > 0 {
		resp["next_before"] = entries[len(entries)-1].ID
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func matchFilters(e accessLogEntry, f accessLogFilters) bool {
	if f.method != "" && e.Method != f.method {
		return false
	}
	if f.path != "" && !strings.Contains(e.Path, f.path) {
		return false
	}
	if f.ip != "" && !strings.Contains(e.ClientIP, f.ip) {
		return false
	}
	if f.country != "" && !strings.EqualFold(e.Country, f.country) {
		return false
	}
	if f.riskMin > 0 && e.RiskScore < f.riskMin {
		return false
	}
	if f.uaType != "" && e.UAType != f.uaType {
		return false
	}
	return true
}

func resolveUsernames(ctx context.Context, entries []accessLogEntry, usersRepo *data.UserRepository) {
	idSet := make(map[uuid.UUID]struct{})
	for i := range entries {
		if entries[i].UserID == "" {
			continue
		}
		uid, err := uuid.Parse(entries[i].UserID)
		if err == nil {
			idSet[uid] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}

	ids := make([]uuid.UUID, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	names, err := usersRepo.GetUsernames(ctx, ids)
	if err != nil || len(names) == 0 {
		return
	}

	for i := range entries {
		if entries[i].UserID == "" {
			continue
		}
		uid, err := uuid.Parse(entries[i].UserID)
		if err != nil {
			continue
		}
		if name, ok := names[uid]; ok {
			entries[i].Username = name
		}
	}
}

func parseAccessLogMessage(id string, values map[string]any) accessLogEntry {
	e := accessLogEntry{ID: id}
	if v, ok := values["ts"].(string); ok {
		e.Timestamp = v
	}
	if v, ok := values["trace_id"].(string); ok {
		e.TraceID = v
	}
	if v, ok := values["method"].(string); ok {
		e.Method = v
	}
	if v, ok := values["path"].(string); ok {
		e.Path = v
	}
	if v, ok := values["status"].(string); ok {
		e.StatusCode, _ = strconv.Atoi(v)
	}
	if v, ok := values["duration_ms"].(string); ok {
		e.DurationMs, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := values["client_ip"].(string); ok {
		e.ClientIP = v
	}
	if v, ok := values["country"].(string); ok {
		e.Country = v
	}
	if v, ok := values["city"].(string); ok {
		e.City = v
	}
	if v, ok := values["user_agent"].(string); ok {
		e.UserAgent = v
	}
	if v, ok := values["ua_type"].(string); ok {
		e.UAType = v
	}
	if v, ok := values["risk_score"].(string); ok {
		e.RiskScore, _ = strconv.Atoi(v)
	}
	if v, ok := values["identity_type"].(string); ok {
		e.IdentityType = v
	}
	if v, ok := values["account_id"].(string); ok {
		e.AccountID = v
	}
	if v, ok := values["user_id"].(string); ok {
		e.UserID = v
	}
	return e
}
