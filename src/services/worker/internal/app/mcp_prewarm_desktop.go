//go:build desktop

package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const desktopMCPConfigChangedTopic = "mcp_config_changed"

type desktopMCPDiscoveryPrewarmTarget struct {
	AccountID    uuid.UUID
	ProfileRef   string
	WorkspaceRef string
}

type desktopMCPDiscoveryPrewarmDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (e *DesktopEngine) StartMCPDiscoveryPrewarm(ctx context.Context) {
	if e == nil || e.db == nil || e.mcpDiscoveryCache == nil {
		return
	}
	go e.prewarmMCPDiscovery(ctx, uuid.Nil)
	go e.listenDesktopMCPConfigChanges(ctx)
}

func (e *DesktopEngine) listenDesktopMCPConfigChanges(ctx context.Context) {
	if e == nil || e.bus == nil || e.mcpDiscoveryCache == nil {
		return
	}
	sub, err := e.bus.Subscribe(ctx, desktopMCPConfigChangedTopic)
	if err != nil {
		slog.WarnContext(ctx, "desktop_mcp_config_subscribe_failed", "error", err.Error())
		return
	}
	defer func() { _ = sub.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.Channel():
			if !ok {
				return
			}
			accountID, err := uuid.Parse(strings.TrimSpace(msg.Payload))
			if err != nil || accountID == uuid.Nil {
				slog.WarnContext(ctx, "desktop_mcp_config_payload_invalid", "payload", strings.TrimSpace(msg.Payload))
				continue
			}
			e.mcpDiscoveryCache.Invalidate(accountID)
			go e.prewarmMCPDiscovery(ctx, accountID)
		}
	}
}

func (e *DesktopEngine) prewarmMCPDiscovery(ctx context.Context, accountID uuid.UUID) {
	targets, err := listDesktopMCPDiscoveryPrewarmTargets(ctx, e.db, accountID)
	if err != nil {
		slog.WarnContext(ctx, "desktop_mcp_prewarm_targets_failed", "error", err.Error())
		return
	}
	for _, target := range targets {
		if ctx.Err() != nil {
			return
		}
		startedAt := time.Now()
		_, _, _, err := e.mcpDiscoveryCache.GetWithMeta(ctx, e.db, target.AccountID, target.ProfileRef, target.WorkspaceRef)
		durationMs := time.Since(startedAt).Milliseconds()
		if err != nil {
			slog.WarnContext(ctx, "desktop_mcp_prewarm_failed",
				"account_id", target.AccountID.String(),
				"profile_ref", target.ProfileRef,
				"workspace_ref", target.WorkspaceRef,
				"duration_ms", durationMs,
				"error", err.Error(),
			)
			continue
		}
	}
}

func listDesktopMCPDiscoveryPrewarmTargets(ctx context.Context, db desktopMCPDiscoveryPrewarmDB, accountID uuid.UUID) ([]desktopMCPDiscoveryPrewarmTarget, error) {
	if db == nil {
		return nil, nil
	}
	sql := `
		SELECT DISTINCT i.account_id, i.profile_ref, w.workspace_ref
		  FROM workspace_mcp_enablements w
		  JOIN profile_mcp_installs i
		    ON i.id = w.install_id
		   AND i.account_id = w.account_id
		 WHERE w.enabled = TRUE
		   AND trim(i.profile_ref) <> ''
		   AND trim(w.workspace_ref) <> ''`
	args := []any{}
	if accountID != uuid.Nil {
		sql += ` AND i.account_id = $1`
		args = append(args, accountID.String())
	}
	sql += ` ORDER BY i.account_id, i.profile_ref, w.workspace_ref`

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := make([]desktopMCPDiscoveryPrewarmTarget, 0)
	for rows.Next() {
		var accountIDText string
		var target desktopMCPDiscoveryPrewarmTarget
		if err := rows.Scan(&accountIDText, &target.ProfileRef, &target.WorkspaceRef); err != nil {
			return nil, err
		}
		parsed, err := uuid.Parse(strings.TrimSpace(accountIDText))
		if err != nil || parsed == uuid.Nil {
			continue
		}
		target.AccountID = parsed
		target.ProfileRef = strings.TrimSpace(target.ProfileRef)
		target.WorkspaceRef = strings.TrimSpace(target.WorkspaceRef)
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
