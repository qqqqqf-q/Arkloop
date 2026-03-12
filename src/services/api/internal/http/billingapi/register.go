package billingapi

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	AuthService         *auth.Service
	AccountMembershipRepo   *data.AccountMembershipRepository
	PlansRepo           *data.PlanRepository
	EntitlementsRepo    *data.EntitlementsRepository
	APIKeysRepo         *data.APIKeysRepository
	SubscriptionsRepo   *data.SubscriptionRepository
	EntitlementService  *entitlement.Service
	UsageRepo           *data.UsageRepository
	CreditsRepo         *data.CreditsRepository
	InviteCodesRepo     *data.InviteCodeRepository
	ReferralsRepo       *data.ReferralRepository
	RedemptionCodesRepo *data.RedemptionCodesRepository
	AuditWriter         *audit.Writer
	Pool                *pgxpool.Pool
}

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/plans", plansEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlansRepo, deps.EntitlementsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/plans/", planEntry(deps.AuthService, deps.AccountMembershipRepo, deps.PlansRepo, deps.EntitlementsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/subscriptions", subscriptionsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.SubscriptionsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/subscriptions/", subscriptionEntry(deps.AuthService, deps.AccountMembershipRepo, deps.SubscriptionsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/entitlement-overrides", entitlementOverridesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.EntitlementsRepo, deps.EntitlementService, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/entitlement-overrides/", entitlementOverrideEntry(deps.AuthService, deps.AccountMembershipRepo, deps.EntitlementsRepo, deps.EntitlementService, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/orgs/{id}/usage", accountUsageEntry(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/orgs/{id}/usage/daily", accountDailyUsage(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/orgs/{id}/usage/by-model", accountUsageByModel(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/usage/daily", adminGlobalDailyUsage(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/usage/summary", adminGlobalUsageSummary(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/usage/by-model", adminGlobalUsageByModel(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/invite-codes", adminInviteCodesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.InviteCodesRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/invite-codes/", adminInviteCodeEntry(deps.AuthService, deps.AccountMembershipRepo, deps.InviteCodesRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/admin/referrals/tree", adminReferralTree(deps.AuthService, deps.AccountMembershipRepo, deps.ReferralsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/referrals", adminReferralsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.ReferralsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/credits/adjust", adminCreditsAdjust(deps.AuthService, deps.AccountMembershipRepo, deps.CreditsRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/admin/credits/bulk-adjust", adminCreditsBulkAdjust(deps.AuthService, deps.AccountMembershipRepo, deps.CreditsRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/admin/credits/reset-all", adminCreditsResetAll(deps.AuthService, deps.AccountMembershipRepo, deps.CreditsRepo, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/admin/credits", adminCreditsEntry(deps.AuthService, deps.AccountMembershipRepo, deps.CreditsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/redemption-codes/batch", adminRedemptionCodesBatch(deps.AuthService, deps.AccountMembershipRepo, deps.RedemptionCodesRepo, deps.APIKeysRepo, deps.AuditWriter, deps.Pool))
	mux.HandleFunc("/v1/admin/redemption-codes/", adminRedemptionCodeEntry(deps.AuthService, deps.AccountMembershipRepo, deps.RedemptionCodesRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/admin/redemption-codes", adminRedemptionCodesEntry(deps.AuthService, deps.AccountMembershipRepo, deps.RedemptionCodesRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/usage", meUsage(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/usage/daily", meDailyUsage(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/usage/by-model", meUsageByModel(deps.AuthService, deps.AccountMembershipRepo, deps.UsageRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/invite-code/reset", meInviteCodeReset(deps.AuthService, deps.AccountMembershipRepo, deps.InviteCodesRepo, deps.EntitlementService, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/me/invite-code", meInviteCode(deps.AuthService, deps.AccountMembershipRepo, deps.InviteCodesRepo, deps.EntitlementService, deps.APIKeysRepo, deps.AuditWriter))
	mux.HandleFunc("/v1/me/credits", meCredits(deps.AuthService, deps.AccountMembershipRepo, deps.CreditsRepo, deps.APIKeysRepo))
	mux.HandleFunc("/v1/me/redeem", meRedeem(deps.AuthService, deps.AccountMembershipRepo, deps.RedemptionCodesRepo, deps.CreditsRepo, deps.APIKeysRepo, deps.AuditWriter, deps.Pool))
}
