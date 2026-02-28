package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRoutingMiddleware per-run 从 DB 加载路由配置，执行路由决策，构建 LLM Gateway。
func NewRoutingMiddleware(
	staticRouter *routing.ProviderRouter,
	dbPool *pgxpool.Pool,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
	resolver *sharedent.Resolver,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		activeRouter := staticRouter
		var dbCfg routing.ProviderRoutingConfig
		if dbPool != nil {
			loaded, dbErr := routing.LoadRoutingConfigFromDB(ctx, dbPool)
			if dbErr != nil {
				slog.WarnContext(ctx, "routing: per-run db load failed, using static", "err", dbErr.Error())
			} else if len(loaded.Routes) > 0 {
				dbCfg = loaded
				activeRouter = routing.NewProviderRouter(dbCfg)
			}
		}

		byokEnabled := false
		if resolver != nil {
			raw, err := resolver.Resolve(ctx, rc.Run.OrgID, "feature.byok_enabled")
			if err == nil {
				byokEnabled = raw == "true"
			}
		}

		resolveGatewayForRouteID := func(resolveCtx context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
			_ = resolveCtx
			cleaned := strings.TrimSpace(routeID)
			if cleaned == "" {
				if rc.Gateway == nil || rc.SelectedRoute == nil {
					return nil, nil, fmt.Errorf("current route is not initialized")
				}
				return rc.Gateway, rc.SelectedRoute, nil
			}

			routeDecision := activeRouter.Decide(map[string]any{"route_id": cleaned}, byokEnabled)
			if routeDecision.Denied != nil {
				return nil, nil, fmt.Errorf("%s: %s", routeDecision.Denied.Code, routeDecision.Denied.Message)
			}
			if routeDecision.Selected == nil {
				return nil, nil, fmt.Errorf("route not found: %s", cleaned)
			}

			gw, gwErr := gatewayFromCredential(routeDecision.Selected.Credential, stubGateway, emitDebugEvents)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, routeDecision.Selected, nil
		}

		// 优先级链：
		// 1. 用户显式 route_id → Decide() 直接处理
		// 2. Skill.preferred_credential / AgentConfig.Model → 凭证名称查找
		// 3. 兜底 → Decide() fallback
		var decision routing.ProviderRouteDecision
		if _, hasRouteID := rc.InputJSON["route_id"]; hasRouteID {
			decision = activeRouter.Decide(rc.InputJSON, byokEnabled)
		} else {
			credName := rc.PreferredCredentialName
			if credName == "" && rc.AgentConfig != nil && rc.AgentConfig.Model != nil {
				credName = strings.TrimSpace(*rc.AgentConfig.Model)
			}
			if credName != "" && len(dbCfg.Routes) > 0 {
				if route, cred, ok := dbCfg.GetHighestPriorityRouteByCredentialName(credName, rc.InputJSON); ok {
					if cred.Scope == routing.CredentialScopeOrg && !byokEnabled {
						decision = routing.ProviderRouteDecision{
							Denied: &routing.ProviderRouteDenied{
								ErrorClass: "policy.denied",
								Code:       "policy.byok_disabled",
								Message:    "BYOK not enabled for this organization",
							},
						}
					} else {
						decision = routing.ProviderRouteDecision{
							Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred},
						}
					}
				}
			}
			if decision.Selected == nil && decision.Denied == nil {
				decision = activeRouter.Decide(rc.InputJSON, byokEnabled)
			}
		}

		var releaseFn func()
		if releaseSlot != nil {
			run := rc.Run
			releaseFn = func() { releaseSlot(ctx, run) }
		}

		if decision.Denied != nil {
			failed := rc.Emitter.Emit(
				"run.failed",
				decision.Denied.ToRunFailedDataJSON(),
				nil,
				StringPtr(decision.Denied.ErrorClass),
			)
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB)
		}

		selected := decision.Selected
		if selected == nil {
			failed := rc.Emitter.Emit(
				"run.failed",
				map[string]any{
					"error_class": llm.ErrorClassInternalError,
					"code":        "internal.route_missing",
					"message":     "route decision is empty",
				},
				nil,
				StringPtr(llm.ErrorClassInternalError),
			)
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB)
		}

		gateway, err := gatewayFromCredential(selected.Credential, stubGateway, emitDebugEvents)
		if err != nil {
			failed := rc.Emitter.Emit(
				"run.failed",
				map[string]any{
					"error_class": llm.ErrorClassInternalError,
					"code":        "internal.gateway_init_failed",
					"message":     "gateway initialization failed",
				},
				nil,
				StringPtr(llm.ErrorClassInternalError),
			)
			if commitErr := appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB); commitErr != nil {
				return commitErr
			}
			return nil
		}

		rc.Gateway = gateway
		rc.SelectedRoute = selected
		rc.ResolveGatewayForRouteID = resolveGatewayForRouteID

		return next(ctx, rc)
	}
}

func gatewayFromCredential(credential routing.ProviderCredential, stubGateway llm.Gateway, emitDebugEvents bool) (llm.Gateway, error) {
	switch credential.ProviderKind {
	case routing.ProviderKindStub:
		return stubGateway, nil
	case routing.ProviderKindOpenAI:
		apiKey, err := resolveAPIKey(credential)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if credential.BaseURL != nil {
			baseURL = *credential.BaseURL
		}
		apiMode := "auto"
		if credential.OpenAIMode != nil {
			apiMode = *credential.OpenAIMode
		}
		return llm.NewOpenAIGateway(llm.OpenAIGatewayConfig{
			APIKey:          apiKey,
			BaseURL:         baseURL,
			APIMode:         apiMode,
			AdvancedJSON:    credential.AdvancedJSON,
			EmitDebugEvents: emitDebugEvents,
		}), nil
	case routing.ProviderKindAnthropic:
		apiKey, err := resolveAPIKey(credential)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if credential.BaseURL != nil {
			baseURL = *credential.BaseURL
		}
		return llm.NewAnthropicGateway(llm.AnthropicGatewayConfig{
			APIKey:          apiKey,
			BaseURL:         baseURL,
			AdvancedJSON:    credential.AdvancedJSON,
			EmitDebugEvents: emitDebugEvents,
		}), nil
	default:
		return nil, fmt.Errorf("unknown provider_kind: %s", credential.ProviderKind)
	}
}

func resolveAPIKey(credential routing.ProviderCredential) (string, error) {
	if credential.APIKeyValue != nil && strings.TrimSpace(*credential.APIKeyValue) != "" {
		return *credential.APIKeyValue, nil
	}
	return lookupAPIKey(credential.APIKeyEnv)
}

func lookupAPIKey(envName *string) (string, error) {
	if envName == nil || strings.TrimSpace(*envName) == "" {
		return "", fmt.Errorf("missing api_key_env")
	}
	name := strings.TrimSpace(*envName)
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("missing environment variable %s", name)
	}
	return value, nil
}
