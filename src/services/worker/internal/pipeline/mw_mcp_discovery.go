package pipeline

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	"github.com/jackc/pgx/v5"
)

const defaultMCPDiscoverySlowEventMs = 3000

// NewMCPDiscoveryMiddleware 按 account 从 DB 加载 MCP 工具（带缓存），合并到 RunContext 的工具集。
func NewMCPDiscoveryMiddleware(
	discoveryCache *mcp.DiscoveryCache,
	queryer func(*RunContext) mcp.DiscoveryQueryer,
	eventsRepo data.RunEventStore,
	baseToolExecutors map[string]tools.Executor,
	baseAllLlmSpecs []llm.ToolSpec,
	baseAllowlistSet map[string]struct{},
	baseRegistry *tools.Registry,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		runToolExecutors := CopyToolExecutors(baseToolExecutors)
		runAllLlmSpecs := append([]llm.ToolSpec{}, baseAllLlmSpecs...)
		runAllowlistSet := CopyStringSet(baseAllowlistSet)
		runRegistry := baseRegistry

		var accountReg mcp.Registration
		var accountErr error
		var cacheMeta mcp.CacheResult
		var diag mcp.DiscoverDiagnostics
		startedAt := time.Now()
		db := mcp.DiscoveryQueryer(nil)
		if queryer != nil {
			db = queryer(rc)
		}
		if db != nil && discoveryCache != nil {
			accountReg, cacheMeta, diag, accountErr = discoveryCache.GetWithMeta(ctx, db, rc.Run.AccountID, rc.ProfileRef, rc.WorkspaceRef)
		} else if db != nil {
			accountReg, diag, accountErr = mcp.DiscoverFromDBWithDiagnostics(ctx, db, rc.Run.AccountID, rc.ProfileRef, rc.WorkspaceRef, nil)
		}
		durationMs := time.Since(startedAt).Milliseconds()
		if db != nil {
			if accountErr != nil {
				slog.WarnContext(ctx, "mcp discovery failed, falling back to base tools", "account_id", rc.Run.AccountID, "profile_ref", rc.ProfileRef, "workspace_ref", rc.WorkspaceRef, "err", accountErr)
			}
			if accountErr == nil && len(accountReg.Executors) > 0 {
				// 过滤与内置 spawn_agent 系列同名的 MCP 工具，避免后续注册冲突
				filteredSpecs := filterBuiltinConflicts(accountReg.AgentSpecs)
				runRegistry = ForkRegistry(baseRegistry, filteredSpecs)
				for name, exec := range accountReg.Executors {
					if _, builtin := spawnagent.BuiltinNames[name]; builtin {
						continue
					}
					runToolExecutors[name] = exec
				}
				for _, spec := range accountReg.LlmSpecs {
					if _, builtin := spawnagent.BuiltinNames[spec.Name]; builtin {
						continue
					}
					runAllLlmSpecs = append(runAllLlmSpecs, spec)
				}
				for _, spec := range filteredSpecs {
					runAllowlistSet[spec.Name] = struct{}{}
				}
			}
			emitMCPDiscoveryEvent(ctx, rc, eventsRepo, durationMs, cacheMeta, diag, accountErr)
		}

		rc.ToolExecutors = runToolExecutors
		rc.ToolSpecs = runAllLlmSpecs
		rc.AllowlistSet = runAllowlistSet
		rc.ToolRegistry = runRegistry

		return next(ctx, rc)
	}
}

// filterBuiltinConflicts 移除与内置 spawn_agent 工具同名的 MCP spec
func filterBuiltinConflicts(specs []tools.AgentToolSpec) []tools.AgentToolSpec {
	out := make([]tools.AgentToolSpec, 0, len(specs))
	for _, spec := range specs {
		if _, builtin := spawnagent.BuiltinNames[spec.Name]; builtin {
			slog.Debug("mcp tool shadowed by builtin, skipped", "name", spec.Name)
			continue
		}
		out = append(out, spec)
	}
	return out
}

func emitMCPDiscoveryEvent(
	ctx context.Context,
	rc *RunContext,
	eventsRepo data.RunEventStore,
	durationMs int64,
	cacheMeta mcp.CacheResult,
	diag mcp.DiscoverDiagnostics,
	discoverErr error,
) {
	if rc == nil || eventsRepo == nil {
		return
	}
	thresholdMs := loadMCPDiscoverySlowEventMs()
	if discoverErr == nil && durationMs < thresholdMs {
		return
	}
	db := runEventDB(rc)
	if db == nil {
		return
	}
	status := "slow"
	if discoverErr != nil {
		status = "failed"
	}
	ev := rc.Emitter.Emit("run.mcp_discovery", buildMCPDiscoveryEventData(status, durationMs, thresholdMs, cacheMeta, diag, discoverErr), nil, nil)
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.WarnContext(ctx, "mcp discovery event tx begin failed", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); err != nil {
		slog.WarnContext(ctx, "mcp discovery event append failed", "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "mcp discovery event commit failed", "error", err)
		return
	}
	notifyRunEventSubscribers(ctx, rc)
}

func buildMCPDiscoveryEventData(
	status string,
	durationMs int64,
	thresholdMs int64,
	cacheMeta mcp.CacheResult,
	diag mcp.DiscoverDiagnostics,
	discoverErr error,
) map[string]any {
	serverOK := 0
	serverFailed := 0
	serverEmpty := 0
	for _, item := range diag.Servers {
		switch item.Outcome {
		case "ok":
			serverOK++
		case "empty":
			serverEmpty++
		default:
			serverFailed++
		}
	}
	payload := map[string]any{
		"phase":             "completed",
		"status":            status,
		"duration_ms":       durationMs,
		"threshold_ms":      thresholdMs,
		"cache_hit":         cacheMeta.Hit,
		"cache_ttl_seconds": int64(cacheMeta.TTL / time.Second),
		"server_count":      diag.ServerCount,
		"server_ok":         serverOK,
		"server_failed":     serverFailed,
		"server_empty":      serverEmpty,
		"tool_count":        diag.ToolCount,
	}
	if servers := summarizeSlowMCPServers(diag.Servers); len(servers) > 0 {
		payload["servers"] = servers
	}
	if discoverErr != nil {
		payload["error_class"] = classifyMCPDiscoveryError(discoverErr)
		payload["error_message"] = trimMCPDiscoveryError(discoverErr.Error())
	}
	return payload
}

func summarizeSlowMCPServers(items []mcp.ServerDiagnostics) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	filtered := make([]mcp.ServerDiagnostics, 0, len(items))
	for _, item := range items {
		if item.Outcome != "ok" || item.DurationMs >= loadMCPDiscoverySlowEventMs() {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		filtered = append(filtered, items...)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].DurationMs == filtered[j].DurationMs {
			return filtered[i].ServerID < filtered[j].ServerID
		}
		return filtered[i].DurationMs > filtered[j].DurationMs
	})
	if len(filtered) > 3 {
		filtered = filtered[:3]
	}
	out := make([]map[string]any, 0, len(filtered))
	for _, item := range filtered {
		out = append(out, map[string]any{
			"server_id":     item.ServerID,
			"transport":     item.Transport,
			"duration_ms":   item.DurationMs,
			"outcome":       item.Outcome,
			"error_class":   item.ErrorClass,
			"tool_count":    item.ToolCount,
			"reused_client": item.ReusedClient,
		})
	}
	return out
}

func loadMCPDiscoverySlowEventMs() int64 {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_MCP_DISCOVERY_SLOW_EVENT_MS"))
	if raw == "" {
		return defaultMCPDiscoverySlowEventMs
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultMCPDiscoverySlowEventMs
	}
	return value
}

func classifyMCPDiscoveryError(err error) string {
	switch err.(type) {
	case mcp.TimeoutError:
		return "timeout"
	case mcp.DisconnectedError:
		return "disconnected"
	case mcp.ProtocolError:
		return "protocol"
	case mcp.RpcError:
		return "rpc"
	default:
		return "unknown"
	}
}

func trimMCPDiscoveryError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 200 {
		return message
	}
	return message[:200]
}
