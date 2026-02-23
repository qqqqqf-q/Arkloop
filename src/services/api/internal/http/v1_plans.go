package http

import (
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type planResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	DisplayName string                   `json:"display_name"`
	CreatedAt   string                   `json:"created_at"`
	Entitlements []planEntitlementResponse `json:"entitlements,omitempty"`
}

type planEntitlementResponse struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueType string `json:"value_type"`
}

type createPlanRequest struct {
	Name         string                       `json:"name"`
	DisplayName  string                       `json:"display_name"`
	Entitlements []createPlanEntitlementItem   `json:"entitlements"`
}

type createPlanEntitlementItem struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	ValueType string `json:"value_type"`
}

func plansEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	planRepo *data.PlanRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createPlan(w, r, authService, membershipRepo, planRepo, entitlementsRepo, apiKeysRepo)
		case nethttp.MethodGet:
			listPlans(w, r, authService, membershipRepo, planRepo, entitlementsRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func planEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	planRepo *data.PlanRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/plans/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			writeNotFound(w, r)
			return
		}

		planID, err := uuid.Parse(tail)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid plan id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getPlan(w, r, traceID, planID, authService, membershipRepo, planRepo, entitlementsRepo, apiKeysRepo)
		default:
			writeMethodNotAllowed(w, r)
		}
	}
}

func createPlan(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	planRepo *data.PlanRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if planRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformPlansManage, w, traceID) {
		return
	}

	var req createPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "name must not be empty", traceID, nil)
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "display_name must not be empty", traceID, nil)
		return
	}

	plan, err := planRepo.Create(r.Context(), req.Name, req.DisplayName)
	if err != nil {
		WriteError(w, nethttp.StatusConflict, "plans.conflict", err.Error(), traceID, nil)
		return
	}

	// 批量设置 entitlements
	var entitlementResps []planEntitlementResponse
	for _, item := range req.Entitlements {
		pe, err := entitlementsRepo.SetForPlan(r.Context(), plan.ID, item.Key, item.Value, item.ValueType)
		if err != nil {
			WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		entitlementResps = append(entitlementResps, toPlanEntitlementResponse(pe))
	}

	resp := toPlanResponse(plan)
	resp.Entitlements = entitlementResps
	writeJSON(w, traceID, nethttp.StatusCreated, resp)
}

func listPlans(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	planRepo *data.PlanRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if planRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformPlansManage, w, traceID) {
		return
	}

	plans, err := planRepo.List(r.Context())
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]planResponse, 0, len(plans))
	for _, p := range plans {
		pr := toPlanResponse(p)
		ents, err := entitlementsRepo.ListByPlan(r.Context(), p.ID)
		if err == nil {
			for _, e := range ents {
				pr.Entitlements = append(pr.Entitlements, toPlanEntitlementResponse(e))
			}
		}
		resp = append(resp, pr)
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func getPlan(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	planID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	planRepo *data.PlanRepository,
	entitlementsRepo *data.EntitlementsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return
	}
	if planRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := resolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !requirePerm(actor, auth.PermPlatformPlansManage, w, traceID) {
		return
	}

	plan, err := planRepo.GetByID(r.Context(), planID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if plan == nil {
		WriteError(w, nethttp.StatusNotFound, "plans.not_found", "plan not found", traceID, nil)
		return
	}

	resp := toPlanResponse(*plan)
	ents, err := entitlementsRepo.ListByPlan(r.Context(), plan.ID)
	if err == nil {
		for _, e := range ents {
			resp.Entitlements = append(resp.Entitlements, toPlanEntitlementResponse(e))
		}
	}
	writeJSON(w, traceID, nethttp.StatusOK, resp)
}

func toPlanResponse(p data.Plan) planResponse {
	return planResponse{
		ID:          p.ID.String(),
		Name:        p.Name,
		DisplayName: p.DisplayName,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toPlanEntitlementResponse(pe data.PlanEntitlement) planEntitlementResponse {
	return planEntitlementResponse{
		ID:        pe.ID.String(),
		Key:       pe.Key,
		Value:     pe.Value,
		ValueType: pe.ValueType,
	}
}
