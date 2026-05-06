package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/localproviders"
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
	auxGateway llm.Gateway,
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
		rc.RoutingByokEnabled = byokEnabled

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

			gw, gwErr := gatewayFromSelectedRoute(*routeDecision.Selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
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
			selected, err := resolveSelectedRouteBySelector(selectorConfig, cleanedSelector, map[string]any{}, byokEnabled)
			if err != nil {
				return nil, nil, err
			}
			if selected == nil {
				return nil, nil, fmt.Errorf("route not found for selector: %s", cleanedSelector)
			}
			gw, gwErr := gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, selected, nil
		}

		var decision routing.ProviderRouteDecision
		if rc.ResumePromptSnapshot != nil && strings.TrimSpace(rc.ResumePromptSnapshot.SelectedRouteID) != "" {
			decision = activeRouter.Decide(map[string]any{"route_id": strings.TrimSpace(rc.ResumePromptSnapshot.SelectedRouteID)}, byokEnabled, false)
		} else if rawOutputRouteID, ok := rc.InputJSON["output_route_id"].(string); ok && strings.TrimSpace(rawOutputRouteID) != "" {
			decision = activeRouter.Decide(map[string]any{"route_id": strings.TrimSpace(rawOutputRouteID)}, byokEnabled, false)
		} else if _, hasRouteID := rc.InputJSON["route_id"]; hasRouteID {
			decision = activeRouter.Decide(rc.InputJSON, byokEnabled, false)
		} else {
			selector := ""
			userModelOverride := false
			// model override from input_json (user-specified) takes priority over persona default
			if rc.ResumePromptSnapshot != nil && strings.TrimSpace(rc.ResumePromptSnapshot.SelectedModel) != "" {
				selector = strings.TrimSpace(rc.ResumePromptSnapshot.SelectedModel)
				userModelOverride = true
			} else if outputModelKey, ok := rc.InputJSON["output_model_key"].(string); ok && strings.TrimSpace(outputModelKey) != "" {
				selector = strings.TrimSpace(outputModelKey)
				userModelOverride = true
			} else if modelOverride, ok := rc.InputJSON["model"].(string); ok && strings.TrimSpace(modelOverride) != "" {
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

		var (
			gateway                  llm.Gateway
			gatewayErr               error
			estimateProviderReqBytes func(llm.Request) (int, error)
		)
		if selected.Credential.ProviderKind == routing.ProviderKindStub {
			gateway = auxGateway
		} else if isLocalProviderKind(selected.Credential.ProviderKind) {
			gateway, gatewayErr = gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gatewayErr == nil {
				resolvedCfg, resolveErr := ResolveGatewayConfigFromSelectedRoute(*selected, emitDebugEvents, rc.LlmMaxResponseBytes)
				if resolveErr == nil {
					estimateProviderReqBytes = func(req llm.Request) (int, error) {
						return llm.EstimateProviderPayloadBytes(resolvedCfg, req)
					}
				}
			}
		} else {
			resolvedCfg, resolveErr := ResolveGatewayConfigFromSelectedRoute(*selected, emitDebugEvents, rc.LlmMaxResponseBytes)
			if resolveErr != nil {
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
			gateway, gatewayErr = llm.NewGatewayFromResolvedConfig(resolvedCfg)
			if gatewayErr == nil {
				estimateProviderReqBytes = func(req llm.Request) (int, error) {
					return llm.EstimateProviderPayloadBytes(resolvedCfg, req)
				}
			}
		}
		if gatewayErr != nil {
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

		inputModel := ""
		if rawModel, ok := rc.InputJSON["model"].(string); ok {
			inputModel = strings.TrimSpace(rawModel)
		}

		rc.Gateway = gateway
		rc.SelectedRoute = selected
		rc.ContextWindowTokens = routing.RouteContextWindowTokens(selected.Route)
		if rc.Temperature == nil {
			rc.Temperature = routing.RouteDefaultTemperature(selected.Route)
		}
		rc.EstimateProviderRequestBytes = estimateProviderReqBytes
		slog.InfoContext(ctx, "routing_selected_model",
			"run_id", rc.Run.ID.String(),
			"thread_id", rc.Run.ThreadID.String(),
			"input_model", inputModel,
			"selected_route_id", selected.Route.ID,
			"selected_model", selected.Route.Model,
			"provider_kind", string(selected.Credential.ProviderKind),
			"credential_name", selected.Credential.Name,
		)
		rc.ResolveGatewayForRouteID = resolveGatewayForRouteID
		rc.ResolveGatewayForAgentName = resolveGatewayForAgentName
		emitTraceEvent(rc, "routing", "routing.selected", map[string]any{
			"model":          selected.Route.Model,
			"provider":       string(selected.Credential.ProviderKind),
			"byok":           byokEnabled,
			"context_window": routing.RouteContextWindowTokens(selected.Route),
		})

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

func gatewayFromSelectedRoute(selected routing.SelectedProviderRoute, auxGateway llm.Gateway, emitDebugEvents bool, llmMaxResponseBytes int) (llm.Gateway, error) {
	return GatewayFromSelectedRoute(selected, auxGateway, emitDebugEvents, llmMaxResponseBytes)
}

func GatewayFromSelectedRoute(selected routing.SelectedProviderRoute, auxGateway llm.Gateway, emitDebugEvents bool, llmMaxResponseBytes int) (llm.Gateway, error) {
	if selected.Credential.ProviderKind == routing.ProviderKindStub {
		return auxGateway, nil
	}
	if isLocalProviderKind(selected.Credential.ProviderKind) {
		return &localProviderGateway{
			selected:            selected,
			emitDebugEvents:     emitDebugEvents,
			llmMaxResponseBytes: llmMaxResponseBytes,
			resolver:            localproviders.NewResolver(localproviders.Options{}),
		}, nil
	}
	resolved, err := ResolveGatewayConfigFromSelectedRoute(selected, emitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		return nil, err
	}
	return llm.NewGatewayFromResolvedConfig(resolved)
}

func ResolveGatewayConfigFromSelectedRoute(selected routing.SelectedProviderRoute, emitDebugEvents bool, llmMaxResponseBytes int) (llm.ResolvedGatewayConfig, error) {
	credential := selected.Credential
	mergedAdvancedJSON := mergeAdvancedJSON(credential.AdvancedJSON, selected.Route.AdvancedJSON)
	customHeaders := extractCustomHeaders(mergedAdvancedJSON)
	advancedJSON := providerPayloadAdvancedJSON(mergedAdvancedJSON)
	if isLocalProviderKind(credential.ProviderKind) {
		return resolveLocalProviderGatewayConfig(selected, localproviders.Credential{ProviderID: localProviderIDFromKind(credential.ProviderKind), AuthMode: localproviders.AuthModeAPIKey}, advancedJSON, emitDebugEvents, llmMaxResponseBytes)
	}
	apiKey, err := resolveAPIKey(credential)
	if err != nil {
		return llm.ResolvedGatewayConfig{}, err
	}
	baseURL := ""
	if credential.BaseURL != nil {
		baseURL = *credential.BaseURL
	}
	transport := llm.TransportConfig{
		APIKey:           apiKey,
		BaseURL:          baseURL,
		DefaultHeaders:   customHeaders,
		EmitDebugEvents:  emitDebugEvents,
		MaxResponseBytes: llmMaxResponseBytes,
	}

	switch credential.ProviderKind {
	case routing.ProviderKindOpenAI:
		apiMode := "auto"
		if credential.OpenAIMode != nil {
			apiMode = *credential.OpenAIMode
		}
		protocol, err := llm.ResolveOpenAIProtocolConfig(apiMode, advancedJSON)
		if err != nil {
			return llm.ResolvedGatewayConfig{}, err
		}
		return llm.ResolvedGatewayConfig{
			ProtocolKind: protocol.PrimaryKind,
			Model:        selected.Route.Model,
			Transport:    transport,
			OpenAI:       &protocol,
		}, nil
	case routing.ProviderKindAnthropic:
		protocol, err := llm.ResolveAnthropicProtocolConfig(advancedJSON)
		if err != nil {
			return llm.ResolvedGatewayConfig{}, err
		}
		return llm.ResolvedGatewayConfig{
			ProtocolKind: llm.ProtocolKindAnthropicMessages,
			Model:        selected.Route.Model,
			Transport:    transport,
			Anthropic:    &protocol,
		}, nil
	case routing.ProviderKindGemini:
		protocol, err := llm.ResolveGeminiProtocolConfig(advancedJSON)
		if err != nil {
			return llm.ResolvedGatewayConfig{}, err
		}
		return llm.ResolvedGatewayConfig{
			ProtocolKind: llm.ProtocolKindGeminiGenerateContent,
			Model:        selected.Route.Model,
			Transport:    transport,
			Gemini:       &protocol,
		}, nil
	case routing.ProviderKindStub:
		return llm.ResolvedGatewayConfig{}, fmt.Errorf("stub route does not resolve to protocol config")
	default:
		return llm.ResolvedGatewayConfig{}, fmt.Errorf("unknown provider_kind: %s", credential.ProviderKind)
	}
}

func ResolveGatewayConfigFromSelectedRouteForRequest(ctx context.Context, selected routing.SelectedProviderRoute, emitDebugEvents bool, llmMaxResponseBytes int) (llm.ResolvedGatewayConfig, error) {
	if !isLocalProviderKind(selected.Credential.ProviderKind) {
		return ResolveGatewayConfigFromSelectedRoute(selected, emitDebugEvents, llmMaxResponseBytes)
	}
	providerID := localProviderIDFromKind(selected.Credential.ProviderKind)
	credential, err := localproviders.NewResolver(localproviders.Options{}).Resolve(ctx, providerID, localproviders.ResolveOptions{Refresh: true})
	if err != nil {
		return llm.ResolvedGatewayConfig{}, err
	}
	advancedJSON := providerPayloadAdvancedJSON(mergeAdvancedJSON(selected.Credential.AdvancedJSON, selected.Route.AdvancedJSON))
	return resolveLocalProviderGatewayConfig(selected, credential, advancedJSON, emitDebugEvents, llmMaxResponseBytes)
}

type localProviderGateway struct {
	selected            routing.SelectedProviderRoute
	emitDebugEvents     bool
	llmMaxResponseBytes int
	resolver            *localproviders.Resolver
}

func (g *localProviderGateway) Stream(ctx context.Context, request llm.Request, yield func(llm.StreamEvent) error) error {
	providerID := localProviderIDFromKind(g.selected.Credential.ProviderKind)
	credential, err := g.resolver.Resolve(ctx, providerID, localproviders.ResolveOptions{Refresh: true})
	if err != nil {
		return yield(llm.StreamRunFailed{Error: llm.GatewayError{ErrorClass: llm.ErrorClassConfigMissing, Message: "local provider credential unavailable"}})
	}
	advancedJSON := providerPayloadAdvancedJSON(mergeAdvancedJSON(g.selected.Credential.AdvancedJSON, g.selected.Route.AdvancedJSON))
	resolved, err := resolveLocalProviderGatewayConfig(g.selected, credential, advancedJSON, g.emitDebugEvents, g.llmMaxResponseBytes)
	if err != nil {
		return yield(llm.StreamRunFailed{Error: llm.GatewayError{ErrorClass: llm.ErrorClassConfigInvalid, Message: "local provider configuration invalid", Details: map[string]any{"reason": err.Error()}}})
	}
	inner, err := llm.NewGatewayFromResolvedConfig(resolved)
	if err != nil {
		return yield(llm.StreamRunFailed{Error: llm.GatewayError{ErrorClass: llm.ErrorClassConfigInvalid, Message: "local provider gateway initialization failed"}})
	}
	if credential.ProviderID == localproviders.ClaudeCodeProviderID && credential.AuthMode == localproviders.AuthModeOAuth {
		request = withClaudeCodeIdentityPrompt(request)
	}
	return inner.Stream(ctx, request, yield)
}

func resolveLocalProviderGatewayConfig(
	selected routing.SelectedProviderRoute,
	credential localproviders.Credential,
	advancedJSON map[string]any,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
) (llm.ResolvedGatewayConfig, error) {
	transport := llm.TransportConfig{
		EmitDebugEvents:  emitDebugEvents,
		MaxResponseBytes: llmMaxResponseBytes,
	}
	switch credential.ProviderID {
	case localproviders.ClaudeCodeProviderID:
		if strings.TrimSpace(credential.BaseURL) != "" {
			transport.BaseURL = strings.TrimSpace(credential.BaseURL)
		}
		protocol, err := llm.ResolveAnthropicProtocolConfig(advancedJSON)
		if err != nil {
			return llm.ResolvedGatewayConfig{}, err
		}
		if credential.AuthMode == localproviders.AuthModeOAuth {
			transport.APIKey = credential.AccessToken
			transport.AuthScheme = "bearer"
			transport.DefaultHeaders = map[string]string{
				"anthropic-beta": "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14",
				"user-agent":     "claude-cli/2.1.2 (external, cli)",
				"x-app":          "cli",
			}
		} else {
			transport.APIKey = credential.APIKey
		}
		return llm.ResolvedGatewayConfig{
			ProtocolKind: llm.ProtocolKindAnthropicMessages,
			Model:        selected.Route.Model,
			Transport:    transport,
			Anthropic:    &protocol,
		}, nil
	case localproviders.CodexProviderID:
		apiMode := "auto"
		if credential.AuthMode == localproviders.AuthModeOAuth {
			transport.APIKey = credential.AccessToken
			transport.BaseURL = "https://chatgpt.com/backend-api"
			transport.DefaultHeaders = map[string]string{}
			if strings.TrimSpace(credential.AccountID) != "" {
				transport.DefaultHeaders["chatgpt-account-id"] = strings.TrimSpace(credential.AccountID)
			}
			protocol, err := llm.ResolveOpenAIProtocolConfig("responses", advancedJSON)
			if err != nil {
				return llm.ResolvedGatewayConfig{}, err
			}
			protocol.PrimaryKind = llm.ProtocolKindOpenAICodexResponses
			protocol.FallbackKind = nil
			return llm.ResolvedGatewayConfig{
				ProtocolKind: llm.ProtocolKindOpenAICodexResponses,
				Model:        selected.Route.Model,
				Transport:    transport,
				OpenAI:       &protocol,
			}, nil
		} else {
			transport.APIKey = credential.APIKey
		}
		protocol, err := llm.ResolveOpenAIProtocolConfig(apiMode, advancedJSON)
		if err != nil {
			return llm.ResolvedGatewayConfig{}, err
		}
		return llm.ResolvedGatewayConfig{
			ProtocolKind: protocol.PrimaryKind,
			Model:        selected.Route.Model,
			Transport:    transport,
			OpenAI:       &protocol,
		}, nil
	default:
		return llm.ResolvedGatewayConfig{}, fmt.Errorf("unknown local provider: %s", credential.ProviderID)
	}
}

func withClaudeCodeIdentityPrompt(request llm.Request) llm.Request {
	block := llm.PromptPlanBlock{
		Name:   "claude_code_identity",
		Target: llm.PromptTargetSystemPrefix,
		Role:   "system",
		Text:   "You are Claude Code, Anthropic's official CLI for Claude.",
	}
	if request.PromptPlan == nil {
		request.PromptPlan = &llm.PromptPlan{}
	}
	request.PromptPlan.SystemBlocks = append([]llm.PromptPlanBlock{block}, request.PromptPlan.SystemBlocks...)
	return request
}

func isLocalProviderKind(kind routing.ProviderKind) bool {
	return kind == routing.ProviderKindClaudeLocal || kind == routing.ProviderKindCodexLocal
}

func localProviderIDFromKind(kind routing.ProviderKind) string {
	switch kind {
	case routing.ProviderKindClaudeLocal:
		return localproviders.ClaudeCodeProviderID
	case routing.ProviderKindCodexLocal:
		return localproviders.CodexProviderID
	default:
		return ""
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
		if key == "openviking_extra_headers" {
			merged[key] = mergeAdvancedHeaderJSON(providerAdvancedJSON[key], value)
			continue
		}
		merged[key] = value
	}
	return merged
}

func mergeAdvancedHeaderJSON(providerHeaders any, modelHeaders any) any {
	providerMap, providerOK := providerHeaders.(map[string]any)
	modelMap, modelOK := modelHeaders.(map[string]any)
	if !providerOK || !modelOK {
		return modelHeaders
	}
	merged := make(map[string]any, len(providerMap)+len(modelMap))
	for key, value := range providerMap {
		merged[key] = value
	}
	for key, value := range modelMap {
		merged[key] = value
	}
	return merged
}

func providerPayloadAdvancedJSON(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	filtered := make(map[string]any, len(raw))
	for key, value := range raw {
		if isInternalAdvancedJSONKey(key) {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

func extractCustomHeaders(raw map[string]any) map[string]string {
	rawHeaders, ok := raw["openviking_extra_headers"].(map[string]any)
	if !ok || len(rawHeaders) == 0 {
		return nil
	}
	headers := make(map[string]string, len(rawHeaders))
	for key, value := range rawHeaders {
		headerName := strings.TrimSpace(key)
		headerValue, ok := value.(string)
		if headerName == "" || !ok || strings.TrimSpace(headerValue) == "" {
			continue
		}
		headers[headerName] = strings.TrimSpace(headerValue)
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func isInternalAdvancedJSONKey(key string) bool {
	switch key {
	case "available_catalog",
		"openviking_backend",
		"openviking_extra_headers",
		"source",
		"local_provider_id",
		"auth_mode",
		"read_only":
		return true
	default:
		return false
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
