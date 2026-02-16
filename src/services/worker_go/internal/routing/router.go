package routing

import (
	"strings"

	"arkloop/services/worker_go/internal/tools"
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

func (r *ProviderRouter) Decide(inputJSON map[string]any, byokEnabled bool) ProviderRouteDecision {
	requestedRoute, exists := inputJSON["route_id"]
	if exists && requestedRoute != nil {
		routeText, ok := requestedRoute.(string)
		if !ok {
			return ProviderRouteDecision{
				Denied: &ProviderRouteDenied{
					ErrorClass: tools.PolicyDeniedCode,
					Code:       "policy.invalid_route_id",
					Message:    "route_id 必须为字符串",
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
						Message:    "路由不存在",
						Details:    map[string]any{"route_id": routeID},
					},
				}
			}
			credential, _ := r.config.GetCredential(route.CredentialID)
			if credential.Scope == CredentialScopeOrg && !byokEnabled {
				return ProviderRouteDecision{
					Denied: &ProviderRouteDenied{
						ErrorClass: tools.PolicyDeniedCode,
						Code:       "policy.byok_disabled",
						Message:    "该组织未启用 BYOK",
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
	credential, _ := r.config.GetCredential(selectedRoute.CredentialID)
	if credential.Scope == CredentialScopeOrg && !byokEnabled {
		return ProviderRouteDecision{
			Denied: &ProviderRouteDenied{
				ErrorClass: tools.PolicyDeniedCode,
				Code:       "policy.byok_disabled",
				Message:    "该组织未启用 BYOK",
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
		if route.Matches(inputJSON) {
			return route
		}
	}
	route, _ := r.config.GetRoute(r.config.DefaultRouteID)
	return route
}
