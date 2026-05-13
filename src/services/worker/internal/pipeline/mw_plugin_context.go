package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pluginQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type pluginEnablementRecord struct {
	PluginID         string
	SettingsJSON     json.RawMessage
	RuntimeStateJSON json.RawMessage
	ManifestJSON     json.RawMessage
}

func NewPluginContextMiddleware(queryer pluginQueryer) RunMiddleware {
	return NewPluginContextMiddlewareWithLoader(func(ctx context.Context, rc *RunContext) ([]PromptSegment, error) {
		return LoadPluginContextSegments(ctx, queryer, rc)
	})
}

func NewPluginContextMiddlewareWithLoader(loader PluginContextLoader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		rc.RemovePromptSegmentsByPrefix("plugin.context.")
		rc.PluginContext = nil
		if loader == nil || rc == nil || rc.Run.AccountID == uuid.Nil {
			return next(ctx, rc)
		}
		segments, err := loader(ctx, rc)
		if err != nil {
			tracePluginContext(rc, "failed", "", err.Error())
			return next(ctx, rc)
		}
		for _, segment := range segments {
			rc.AppendPromptSegment(segment)
			rc.PluginContext = append(rc.PluginContext, segment)
		}
		tracePluginContext(rc, "completed", "", "")
		return next(ctx, rc)
	}
}

func LoadPluginContextSegments(ctx context.Context, queryer pluginQueryer, rc *RunContext) ([]PromptSegment, error) {
	records, err := loadPluginEnablements(ctx, queryer, rc)
	if err != nil {
		if isPluginSchemaUnavailable(err) {
			tracePluginContext(rc, "skipped", "", err.Error())
			return nil, nil
		}
		return nil, err
	}
	segments := make([]PromptSegment, 0, len(records))
	for _, record := range records {
		content, ok := pluginManifestContextContent(record.ManifestJSON)
		if !ok {
			tracePluginContext(rc, "skipped", record.PluginID, "context_content_unavailable")
			continue
		}
		segments = append(segments, PromptSegment{
			Name:          "plugin.context." + sanitizePromptSegmentName(record.PluginID),
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          content,
			Stability:     PromptStabilitySessionPrefix,
			CacheEligible: true,
		})
	}
	return segments, nil
}

func loadPluginEnablements(ctx context.Context, queryer pluginQueryer, rc *RunContext) ([]pluginEnablementRecord, error) {
	if queryer == nil || rc == nil || rc.Run.AccountID == uuid.Nil || strings.TrimSpace(rc.ProfileRef) == "" || strings.TrimSpace(rc.WorkspaceRef) == "" {
		return nil, nil
	}
	rows, err := queryer.Query(ctx, `
		SELECT e.plugin_id,
		       COALESCE(e.settings_json, '{}'::jsonb),
		       COALESCE(s.status_json, '{}'::jsonb),
		       p.manifest_json
		  FROM plugin_enablements e
		  JOIN plugin_packages p
		    ON p.id = e.package_id
		   AND p.account_id = e.account_id
		   AND p.is_active = TRUE
		  LEFT JOIN plugin_runtime_state s
		    ON s.account_id = e.account_id
		   AND s.package_id = e.package_id
		   AND s.profile_ref = e.profile_ref
		   AND s.workspace_ref = e.workspace_ref
		 WHERE e.account_id = $1
		   AND e.profile_ref = $2
		   AND e.workspace_ref = $3
		   AND e.desired_enabled = TRUE
		 ORDER BY e.created_at ASC, e.plugin_id ASC
	`, rc.Run.AccountID, rc.ProfileRef, rc.WorkspaceRef)
	if err != nil {
		return nil, fmt.Errorf("plugin enablements query: %w", err)
	}
	defer rows.Close()

	var records []pluginEnablementRecord
	for rows.Next() {
		var record pluginEnablementRecord
		if err := rows.Scan(&record.PluginID, &record.SettingsJSON, &record.RuntimeStateJSON, &record.ManifestJSON); err != nil {
			return nil, fmt.Errorf("plugin enablement scan: %w", err)
		}
		record.PluginID = strings.TrimSpace(record.PluginID)
		if record.PluginID == "" {
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plugin enablement rows: %w", err)
	}
	return records, nil
}

func pluginManifestContextContent(raw json.RawMessage) (string, bool) {
	manifest := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &manifest) != nil {
		return "", false
	}
	value, ok := manifest["context"]
	if !ok {
		value = manifest["contexts"]
	}
	return pluginContextValueContent(value)
}

func pluginContextValueContent(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return nonPathContent(typed)
	case map[string]any:
		for _, key := range []string{"content", "text", "body"} {
			if content, ok := typed[key].(string); ok {
				return nonPathContent(content)
			}
		}
		return "", false
	case []any:
		var parts []string
		for _, item := range typed {
			content, ok := pluginContextValueContent(item)
			if ok {
				parts = append(parts, content)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n\n"), true
	default:
		return "", false
	}
}

func nonPathContent(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	if !strings.Contains(content, "\n") && (strings.HasSuffix(content, ".md") || strings.HasSuffix(content, ".txt")) {
		return "", false
	}
	return content, true
}

func tracePluginContext(rc *RunContext, status string, pluginID string, err string) {
	fields := map[string]any{"status": strings.TrimSpace(status)}
	if strings.TrimSpace(pluginID) != "" {
		fields["plugin_id"] = strings.TrimSpace(pluginID)
	}
	if strings.TrimSpace(err) != "" {
		fields["error"] = strings.TrimSpace(err)
	}
	emitTraceEvent(rc, "plugin_context", "plugin_context."+strings.TrimSpace(status), fields)
}

func isPluginSchemaUnavailable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "plugin_enablements") ||
		strings.Contains(text, "plugin_packages") ||
		strings.Contains(text, "no such table") ||
		strings.Contains(text, "does not exist")
}
