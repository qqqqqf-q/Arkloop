package app

import (
	"context"
	"strings"

	"arkloop/services/api/internal/observability"
	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/jackc/pgx/v5/pgxpool"
)

func warnUnsafeOutboundBaseURLs(ctx context.Context, pool *pgxpool.Pool, logger *observability.JSONLogger) {
	if pool == nil || logger == nil {
		return
	}
	policy := sharedoutbound.DefaultPolicy()
	checks := []struct {
		table string
		query string
	}{
		{
			table: "asr_credentials",
			query: `SELECT id::text, scope, COALESCE(org_id::text, ''), provider, base_url
				FROM asr_credentials
				WHERE base_url IS NOT NULL AND BTRIM(base_url) <> ''`,
		},
		{
			table: "llm_credentials",
			query: `SELECT id::text, 'org', org_id::text, provider, base_url
				FROM llm_credentials
				WHERE base_url IS NOT NULL AND BTRIM(base_url) <> ''`,
		},
		{
			table: "tool_provider_configs",
			query: `SELECT id::text, scope, COALESCE(org_id::text, ''), provider_name, base_url
				FROM tool_provider_configs
				WHERE base_url IS NOT NULL AND BTRIM(base_url) <> ''`,
		},
	}

	for _, check := range checks {
		rows, err := pool.Query(ctx, check.query)
		if err != nil {
			logger.Warn("outbound_base_url_scan_failed", observability.LogFields{}, map[string]any{"table": check.table, "error": err.Error()})
			continue
		}
		for rows.Next() {
			var (
				id       string
				scope    string
				orgID    string
				provider string
				baseURL  string
			)
			if err := rows.Scan(&id, &scope, &orgID, &provider, &baseURL); err != nil {
				logger.Warn("outbound_base_url_scan_failed", observability.LogFields{}, map[string]any{"table": check.table, "error": err.Error()})
				continue
			}
			normalize := policy.NormalizeBaseURL
			providerName := strings.TrimSpace(provider)
			if check.table == "tool_provider_configs" && (providerName == "memory.openviking" || strings.HasPrefix(providerName, "sandbox.")) {
				normalize = policy.NormalizeInternalBaseURL
			}
			if _, err := normalize(baseURL); err != nil {
				logger.Warn("unsafe_outbound_base_url_configured", observability.LogFields{}, map[string]any{
					"table":    check.table,
					"id":       id,
					"scope":    strings.TrimSpace(scope),
					"org_id":   strings.TrimSpace(orgID),
					"provider": providerName,
					"reason":   err.Error(),
				})
			}
		}
		if err := rows.Err(); err != nil {
			logger.Warn("outbound_base_url_scan_failed", observability.LogFields{}, map[string]any{"table": check.table, "error": err.Error()})
		}
		rows.Close()
	}
}
