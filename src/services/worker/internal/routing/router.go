package routing

import (
	"strings"

	"arkloop/services/worker/internal/tools"
)

type SelectedProviderRoute struct {
	Route      ProviderRouteRule
	Credential ProviderCredential
}

func (s SelectedProviderRoute) ToRunEventDataJSON() map[string]any {
	payload := map[string]any{
		"route_id": s.Route.ID,
		"model":    s.Route.Model,
	}
	for key, value := range s.Credential.ToPublicJSON() {
		payload[key] = value
	}
	return payload
}

type ProviderRouteDenied struct {
	ErrorClass string
	Code       string
	Message    string
	Details    map[string]any
}

func (d ProviderRouteDenied) ToRunFailedDataJSON() map[string]any {
	payload := map[string]any{
		"error_class": d.ErrorClass,
		"code":        d.Code,
		"message":     d.Message,
	}
	if len(d.Details) > 0 {
		payload["details"] = d.Details
	}
	return payload
}

type ProviderRouteDecision struct {
	Selected *SelectedProviderRoute
	Denied   *ProviderRouteDenied
}

type ProviderRouter struct {
	config ProviderRoutingConfig
}

func NewProviderRouter(config ProviderRoutingConfig) *ProviderRouter {
	return &ProviderRouter{config: config}
}

func (r *ProviderRouter) Config() ProviderRoutingConfig {
	if r == nil {
		return ProviderRoutingConfig{}
	}
	return r.config
}

func (r *ProviderRouter) Decide(inputJSON map[string]any, byokEnabled bool) ProviderRouteDecision {
	requestedRoute, exists := inputJSON["route_id"]
	if exists && requestedRoute != nil {
		routeText, ok := requestedRoute.(string)
		if !ok {
			return ProviderRouteDecision{
				Denied: &ProviderRouteDenied{
					ErrorClass: tools.PolicyDeniedCode,
					Code:       "policy.invalid_route_id",
					Message:    "route_id must be a string",
				},
			}
		}
		if strings.TrimSpace(routeText) != "" {
			routeID := strings.TrimSpace(routeText)
			route, ok := r.config.GetRoute(routeID)
			if !ok {
				return ProviderRouteDecision{
					Denied: &ProviderRouteDenied{
						ErrorClass: tools.PolicyDeniedCode,
						Code:       "policy.route_not_found",
						Message:    "route not found",
						Details:    map[string]any{"route_id": routeID},
					},
				}
			}
			credential, _ := r.config.GetCredential(route.CredentialID)
			if credential.Scope == CredentialScopeProject && !byokEnabled {
				return ProviderRouteDecision{
					Denied: &ProviderRouteDenied{
						ErrorClass: tools.PolicyDeniedCode,
						Code:       "policy.byok_disabled",
						Message:    "BYOK not enabled for this project",
						Details: map[string]any{
							"route_id":      route.ID,
							"credential_id": credential.ID,
						},
					},
				}
			}
			return ProviderRouteDecision{
				Selected: &SelectedProviderRoute{Route: route, Credential: credential},
			}
		}
	}

	selectedRoute := r.pickFirstMatchingRoute(inputJSON)
	if selectedRoute.ID == "" {
		// DB 配置无全局默认，未匹配任何路由
		return ProviderRouteDecision{Selected: nil}
	}
	credential, _ := r.config.GetCredential(selectedRoute.CredentialID)
	if credential.Scope == CredentialScopeProject && !byokEnabled {
		return ProviderRouteDecision{
			Denied: &ProviderRouteDenied{
				ErrorClass: tools.PolicyDeniedCode,
				Code:       "policy.byok_disabled",
				Message:    "BYOK not enabled for this project",
				Details: map[string]any{
					"route_id":      selectedRoute.ID,
					"credential_id": credential.ID,
				},
			},
		}
	}
	return ProviderRouteDecision{
		Selected: &SelectedProviderRoute{Route: selectedRoute, Credential: credential},
	}
}

func (r *ProviderRouter) pickFirstMatchingRoute(inputJSON map[string]any) ProviderRouteRule {
	for _, route := range r.config.Routes {
		if route.ID == r.config.DefaultRouteID {
			continue
		}
		// 空 when 的路由只能通过 route_id 显式选择，不参与自动匹配
		if len(route.When) == 0 {
			continue
		}
		if route.Matches(inputJSON) {
			return route
		}
	}
	if strings.TrimSpace(r.config.DefaultRouteID) != "" {
		route, _ := r.config.GetRoute(r.config.DefaultRouteID)
		return route
	}
	if len(r.config.Routes) > 0 {
		return r.config.Routes[0]
	}
	return ProviderRouteRule{}
}
