package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

type RouteNotFoundError struct {
	Selector string
}

func (e *RouteNotFoundError) Error() string {
	return fmt.Sprintf("route not found for selector: %s", e.Selector)
}

func NewRoutingMiddleware(
	staticRouter *routing.ProviderRouter,
	configLoader *routing.ConfigLoader,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
	resolver *sharedent.Resolver,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		activeRouter := staticRouter
		selectorConfig := routing.ProviderRoutingConfig{}
		if staticRouter != nil {
			selectorConfig = staticRouter.Config()
		}
		if configLoader != nil {
			loaded, dbErr := configLoader.Load(ctx, &rc.Run.AccountID)
			if dbErr != nil {
				slog.WarnContext(ctx, "routing: per-run load failed, using static", "err", dbErr.Error())
			} else if len(loaded.Routes) > 0 {
				selectorConfig = loaded
				activeRouter = routing.NewProviderRouter(loaded)
			}
		}

		platformSelectorConfig := selectorConfig.PlatformOnly()

		byokEnabled := false
		if resolver != nil {
			raw, err := resolver.Resolve(ctx, rc.Run.AccountID, "feature.byok_enabled")
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

			routeDecision := activeRouter.Decide(map[string]any{"route_id": cleaned}, byokEnabled, false)
			if routeDecision.Denied != nil {
				return nil, nil, fmt.Errorf("%s: %s", routeDecision.Denied.Code, routeDecision.Denied.Message)
			}
			if routeDecision.Selected == nil {
				return nil, nil, fmt.Errorf("route not found: %s", cleaned)
			}

			gw, gwErr := gatewayFromSelectedRoute(*routeDecision.Selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, routeDecision.Selected, nil
		}

		resolveGatewayForAgentName := func(resolveCtx context.Context, selector string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
			_ = resolveCtx
			cleanedSelector := strings.TrimSpace(selector)
			if cleanedSelector == "" {
				if rc.Gateway == nil || rc.SelectedRoute == nil {
					return nil, nil, fmt.Errorf("current route is not initialized")
				}
				return rc.Gateway, rc.SelectedRoute, nil
			}
			selected, err := resolveSelectedRouteBySelector(platformSelectorConfig, cleanedSelector, map[string]any{}, byokEnabled)
			if err != nil {
				return nil, nil, err
			}
			if selected == nil {
				return nil, nil, fmt.Errorf("route not found for selector: %s", cleanedSelector)
			}
			gw, gwErr := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, selected, nil
		}

	var decision routing.ProviderRouteDecision
	if _, hasRouteID := rc.InputJSON["route_id"]; hasRouteID {
		decision = activeRouter.Decide(rc.InputJSON, byokEnabled, false)
	} else {
		selector := ""
		userModelOverride := false
		// model override from input_json (user-specified) takes priority over persona default
		if modelOverride, ok := rc.InputJSON["model"].(string); ok && strings.TrimSpace(modelOverride) != "" {
			selector = strings.TrimSpace(modelOverride)
			userModelOverride = true
		} else if rc.AgentConfig != nil && rc.AgentConfig.Model != nil {
			selector = strings.TrimSpace(*rc.AgentConfig.Model)
		}
			if selector != "" {
				// user-specified overrides must be able to resolve BYOK (user-scope) routes;
				// persona-configured selectors only resolve against platform routes.
				cfgForSelector := platformSelectorConfig
				if userModelOverride {
					cfgForSelector = selectorConfig
				}
				selected, err := resolveSelectedRouteBySelector(cfgForSelector, selector, rc.InputJSON, byokEnabled)
				if err != nil {
					var notFound *RouteNotFoundError
					if errors.As(err, &notFound) {
						decision = routing.ProviderRouteDecision{
							Denied: &routing.ProviderRouteDenied{
								ErrorClass: llm.ErrorClassRoutingNotFound,
								Code:       "routing.model_not_found",
								Message:    err.Error(),
							},
						}
					} else {
						decision = routing.ProviderRouteDecision{
							Denied: &routing.ProviderRouteDenied{
								ErrorClass: llm.ErrorClassRoutingNotFound,
								Code:       "routing.not_found",
								Message:    err.Error(),
							},
						}
					}
				} else if selected != nil {
					decision = routing.ProviderRouteDecision{Selected: selected}
				}
			}
			if decision.Selected == nil && decision.Denied == nil && rc.PreferredCredentialName != "" {
				if route, cred, ok := platformSelectorConfig.GetHighestPriorityRouteByCredentialName(rc.PreferredCredentialName, rc.InputJSON); ok {
					if denied := denyByokIfNeeded(cred, byokEnabled); denied != nil {
						decision = routing.ProviderRouteDecision{Denied: denied}
					} else {
						decision = routing.ProviderRouteDecision{Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred}}
					}
				}
			}
			if decision.Selected == nil && decision.Denied == nil {
				decision = activeRouter.Decide(rc.InputJSON, byokEnabled, true)
			}
		}

		var releaseFn func()
		if rc.ReleaseSlot != nil {
			releaseFn = rc.ReleaseSlot
		} else if releaseSlot != nil {
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
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
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
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
		}

		gateway, err := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
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
			if commitErr := appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus); commitErr != nil {
				return commitErr
			}
			return nil
		}

		rc.Gateway = gateway
		rc.SelectedRoute = selected
		rc.ResolveGatewayForRouteID = resolveGatewayForRouteID
		rc.ResolveGatewayForAgentName = resolveGatewayForAgentName

		return next(ctx, rc)
	}
}

func resolveSelectedRouteBySelector(cfg routing.ProviderRoutingConfig, selector string, inputJSON map[string]any, byokEnabled bool) (*routing.SelectedProviderRoute, error) {
	credentialName, modelName, exact := splitModelSelector(selector)
	if exact {
		route, cred, ok := cfg.GetHighestPriorityRouteByCredentialAndModel(credentialName, modelName, inputJSON)
		if !ok {
			return nil, &RouteNotFoundError{Selector: selector}
		}
		if denied := denyByokIfNeeded(cred, byokEnabled); denied != nil {
			return nil, fmt.Errorf("%s: %s", denied.Code, denied.Message)
		}
		return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
	}

	route, cred, ok := cfg.GetHighestPriorityRouteByModel(selector, inputJSON)
	if !ok {
		return nil, nil
	}
	if denied := denyByokIfNeeded(cred, byokEnabled); denied != nil {
		return nil, fmt.Errorf("%s: %s", denied.Code, denied.Message)
	}
	return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
}

func splitModelSelector(selector string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(selector), "^", 2)
	if len(parts) != 2 {
		return "", strings.TrimSpace(selector), false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	if left == "" || right == "" {
		return "", strings.TrimSpace(selector), false
	}
	return left, right, true
}

func denyByokIfNeeded(cred routing.ProviderCredential, byokEnabled bool) *routing.ProviderRouteDenied {
	if cred.OwnerKind == routing.CredentialScopeUser && !byokEnabled {
		return &routing.ProviderRouteDenied{
			ErrorClass: llm.ErrorClassRuntimePolicyDenied,
			Code:       "policy.byok_disabled",
			Message:    "BYOK not enabled",
		}
	}
	return nil
}

func gatewayFromSelectedRoute(selected routing.SelectedProviderRoute, stubGateway llm.Gateway, emitDebugEvents bool, llmMaxResponseBytes int) (llm.Gateway, error) {
	credential := selected.Credential
	advancedJSON := mergeAdvancedJSON(credential.AdvancedJSON, selected.Route.AdvancedJSON)
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
			AdvancedJSON:    advancedJSON,
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
			APIKey:           apiKey,
			BaseURL:          baseURL,
			AdvancedJSON:     advancedJSON,
			EmitDebugEvents:  emitDebugEvents,
			MaxResponseBytes: llmMaxResponseBytes,
		}), nil
	default:
		return nil, fmt.Errorf("unknown provider_kind: %s", credential.ProviderKind)
	}
}

func mergeAdvancedJSON(providerAdvancedJSON map[string]any, modelAdvancedJSON map[string]any) map[string]any {
	if len(providerAdvancedJSON) == 0 && len(modelAdvancedJSON) == 0 {
		return map[string]any{}
	}
	merged := make(map[string]any, len(providerAdvancedJSON)+len(modelAdvancedJSON))
	for key, value := range providerAdvancedJSON {
		merged[key] = value
	}
	for key, value := range modelAdvancedJSON {
		merged[key] = value
	}
	return merged
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
