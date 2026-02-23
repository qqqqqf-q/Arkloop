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
		if dbPool != nil {
			dbCfg, dbErr := routing.LoadRoutingConfigFromDB(ctx, dbPool)
			if dbErr != nil {
				slog.WarnContext(ctx, "routing: per-run db load failed, using static", "err", dbErr.Error())
			} else if len(dbCfg.Routes) > 0 {
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

		decision := activeRouter.Decide(rc.InputJSON, byokEnabled)

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
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn)
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
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn)
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
			if commitErr := appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn); commitErr != nil {
				return commitErr
			}
			return nil
		}

		rc.Gateway = gateway
		rc.SelectedRoute = selected

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
