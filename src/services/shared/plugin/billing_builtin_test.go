package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// --- mock 定义 ---

type mockCreditOps struct {
	getBalanceFn func(ctx context.Context, accountID uuid.UUID) (int64, error)
	deductFn     func(ctx context.Context, accountID uuid.UUID, amount int64, txType string, refID uuid.UUID, metadata map[string]any) error
}

func (m *mockCreditOps) GetBalance(ctx context.Context, accountID uuid.UUID) (int64, error) {
	return m.getBalanceFn(ctx, accountID)
}

func (m *mockCreditOps) Deduct(ctx context.Context, accountID uuid.UUID, amount int64, txType string, refID uuid.UUID, metadata map[string]any) error {
	return m.deductFn(ctx, accountID, amount, txType, refID, metadata)
}

type mockSubscriptionOps struct {
	createFn    func(ctx context.Context, accountID uuid.UUID, planID string) error
	cancelFn    func(ctx context.Context, accountID uuid.UUID) error
	getActiveFn func(ctx context.Context, accountID uuid.UUID) (*Subscription, error)
}

func (m *mockSubscriptionOps) Create(ctx context.Context, accountID uuid.UUID, planID string) error {
	return m.createFn(ctx, accountID, planID)
}

func (m *mockSubscriptionOps) Cancel(ctx context.Context, accountID uuid.UUID) error {
	return m.cancelFn(ctx, accountID)
}

func (m *mockSubscriptionOps) GetActive(ctx context.Context, accountID uuid.UUID) (*Subscription, error) {
	return m.getActiveFn(ctx, accountID)
}

type mockQuotaOps struct {
	checkFn func(ctx context.Context, accountID uuid.UUID, resource string) (bool, error)
}

func (m *mockQuotaOps) Check(ctx context.Context, accountID uuid.UUID, resource string) (bool, error) {
	return m.checkFn(ctx, accountID, resource)
}

type mockCreditCalculator struct {
	calculateFn func(ctx context.Context, accountID uuid.UUID, usage UsageRecord) (int64, error)
}

func (m *mockCreditCalculator) Calculate(ctx context.Context, accountID uuid.UUID, usage UsageRecord) (int64, error) {
	return m.calculateFn(ctx, accountID, usage)
}

// --- helper ---

func newTestProvider(t *testing.T) (*BuiltinBillingProvider, *mockCreditOps, *mockSubscriptionOps, *mockQuotaOps, *mockCreditCalculator) {
	t.Helper()
	credits := &mockCreditOps{
		getBalanceFn: func(context.Context, uuid.UUID) (int64, error) { return 0, nil },
		deductFn:     func(context.Context, uuid.UUID, int64, string, uuid.UUID, map[string]any) error { return nil },
	}
	subs := &mockSubscriptionOps{
		createFn:    func(context.Context, uuid.UUID, string) error { return nil },
		cancelFn:    func(context.Context, uuid.UUID) error { return nil },
		getActiveFn: func(context.Context, uuid.UUID) (*Subscription, error) { return nil, nil },
	}
	quotas := &mockQuotaOps{
		checkFn: func(context.Context, uuid.UUID, string) (bool, error) { return true, nil },
	}
	calc := &mockCreditCalculator{
		calculateFn: func(context.Context, uuid.UUID, UsageRecord) (int64, error) { return 0, nil },
	}
	p, err := NewBuiltinBillingProvider(credits, subs, quotas, calc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return p, credits, subs, quotas, calc
}

// --- 测试 ---

func TestNewBuiltinBillingProvider_NilDeps(t *testing.T) {
	credits := &mockCreditOps{}
	subs := &mockSubscriptionOps{}
	quotas := &mockQuotaOps{}
	calc := &mockCreditCalculator{}

	cases := []struct {
		name    string
		credits CreditOps
		subs    SubscriptionOps
		quotas  QuotaOps
		calc    CreditCalculator
		wantMsg string
	}{
		{"nil credits", nil, subs, quotas, calc, "credits must not be nil"},
		{"nil subs", credits, nil, quotas, calc, "subs must not be nil"},
		{"nil quotas", credits, subs, nil, calc, "quotas must not be nil"},
		{"nil calc", credits, subs, quotas, nil, "calculator must not be nil"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewBuiltinBillingProvider(tc.credits, tc.subs, tc.quotas, tc.calc)
			if p != nil {
				t.Error("expected nil provider")
			}
			if err == nil || err.Error() != tc.wantMsg {
				t.Errorf("expected error %q, got %v", tc.wantMsg, err)
			}
		})
	}
}

func TestBuiltinBillingProvider_CreateSubscription(t *testing.T) {
	accountID := uuid.New()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		var gotOrg uuid.UUID
		var gotPlan string
		subs.createFn = func(_ context.Context, org uuid.UUID, plan string) error {
			gotOrg = org
			gotPlan = plan
			return nil
		}
		if err := p.CreateSubscription(ctx, accountID, "pro"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotOrg != accountID {
			t.Errorf("accountID = %v, want %v", gotOrg, accountID)
		}
		if gotPlan != "pro" {
			t.Errorf("planID = %q, want %q", gotPlan, "pro")
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		want := errors.New("db down")
		subs.createFn = func(context.Context, uuid.UUID, string) error { return want }
		if err := p.CreateSubscription(ctx, accountID, "pro"); !errors.Is(err, want) {
			t.Errorf("error = %v, want %v", err, want)
		}
	})
}

func TestBuiltinBillingProvider_CancelSubscription(t *testing.T) {
	accountID := uuid.New()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		var gotOrg uuid.UUID
		subs.cancelFn = func(_ context.Context, org uuid.UUID) error {
			gotOrg = org
			return nil
		}
		if err := p.CancelSubscription(ctx, accountID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotOrg != accountID {
			t.Errorf("accountID = %v, want %v", gotOrg, accountID)
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		want := errors.New("not found")
		subs.cancelFn = func(context.Context, uuid.UUID) error { return want }
		if err := p.CancelSubscription(ctx, accountID); !errors.Is(err, want) {
			t.Errorf("error = %v, want %v", err, want)
		}
	})
}

func TestBuiltinBillingProvider_GetActiveSubscription(t *testing.T) {
	accountID := uuid.New()
	ctx := context.Background()

	t.Run("has subscription", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		want := &Subscription{ID: "sub-1", AccountID: accountID, PlanID: "pro", Status: "active"}
		subs.getActiveFn = func(context.Context, uuid.UUID) (*Subscription, error) { return want, nil }
		got, err := p.GetActiveSubscription(ctx, accountID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("subscription = %+v, want %+v", got, want)
		}
	})

	t.Run("no subscription", func(t *testing.T) {
		p, _, subs, _, _ := newTestProvider(t)
		subs.getActiveFn = func(context.Context, uuid.UUID) (*Subscription, error) { return nil, nil }
		got, err := p.GetActiveSubscription(ctx, accountID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
}

func TestBuiltinBillingProvider_ReportUsage(t *testing.T) {
	accountID := uuid.New()
	runID := uuid.New()
	ctx := context.Background()
	usage := UsageRecord{RunID: runID, TokensIn: 100, TokensOut: 200, ToolCalls: 3, DurationMs: 500}

	t.Run("normal deduction", func(t *testing.T) {
		p, credits, _, _, calc := newTestProvider(t)
		calc.calculateFn = func(context.Context, uuid.UUID, UsageRecord) (int64, error) { return 42, nil }
		var deducted int64
		var gotTxType string
		var gotRefID uuid.UUID
		credits.deductFn = func(_ context.Context, _ uuid.UUID, amount int64, txType string, refID uuid.UUID, _ map[string]any) error {
			deducted = amount
			gotTxType = txType
			gotRefID = refID
			return nil
		}
		if err := p.ReportUsage(ctx, accountID, usage); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deducted != 42 {
			t.Errorf("deducted = %d, want 42", deducted)
		}
		if gotTxType != "run" {
			t.Errorf("txType = %q, want %q", gotTxType, "run")
		}
		if gotRefID != runID {
			t.Errorf("refID = %v, want %v", gotRefID, runID)
		}
	})

	t.Run("zero amount skips deduction", func(t *testing.T) {
		p, credits, _, _, calc := newTestProvider(t)
		calc.calculateFn = func(context.Context, uuid.UUID, UsageRecord) (int64, error) { return 0, nil }
		called := false
		credits.deductFn = func(context.Context, uuid.UUID, int64, string, uuid.UUID, map[string]any) error {
			called = true
			return nil
		}
		if err := p.ReportUsage(ctx, accountID, usage); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called {
			t.Error("deduct should not be called for zero amount")
		}
	})

	t.Run("calculator error", func(t *testing.T) {
		p, _, _, _, calc := newTestProvider(t)
		calcErr := errors.New("calc failed")
		calc.calculateFn = func(context.Context, uuid.UUID, UsageRecord) (int64, error) { return 0, calcErr }
		err := p.ReportUsage(ctx, accountID, usage)
		if !errors.Is(err, calcErr) {
			t.Errorf("error = %v, want wrapping %v", err, calcErr)
		}
	})

	t.Run("deduct error", func(t *testing.T) {
		p, credits, _, _, calc := newTestProvider(t)
		calc.calculateFn = func(context.Context, uuid.UUID, UsageRecord) (int64, error) { return 10, nil }
		deductErr := errors.New("insufficient credits")
		credits.deductFn = func(context.Context, uuid.UUID, int64, string, uuid.UUID, map[string]any) error {
			return deductErr
		}
		if err := p.ReportUsage(ctx, accountID, usage); !errors.Is(err, deductErr) {
			t.Errorf("error = %v, want %v", err, deductErr)
		}
	})
}

func TestBuiltinBillingProvider_CheckQuota(t *testing.T) {
	accountID := uuid.New()
	ctx := context.Background()

	t.Run("allowed", func(t *testing.T) {
		p, _, _, quotas, _ := newTestProvider(t)
		quotas.checkFn = func(context.Context, uuid.UUID, string) (bool, error) { return true, nil }
		ok, err := p.CheckQuota(ctx, accountID, "messages")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected allowed=true")
		}
	})

	t.Run("exceeded", func(t *testing.T) {
		p, _, _, quotas, _ := newTestProvider(t)
		quotas.checkFn = func(context.Context, uuid.UUID, string) (bool, error) { return false, nil }
		ok, err := p.CheckQuota(ctx, accountID, "messages")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected allowed=false")
		}
	})
}

func TestBuiltinBillingProvider_HandleWebhook(t *testing.T) {
	p, _, _, _, _ := newTestProvider(t)
	err := p.HandleWebhook(context.Background(), "stripe", []byte("{}"), "sig")
	if !errors.Is(err, ErrWebhookNotSupported) {
		t.Errorf("error = %v, want %v", err, ErrWebhookNotSupported)
	}
}

func TestBuiltinBillingProvider_ImplementsInterface(t *testing.T) {
	var _ BillingProvider = (*BuiltinBillingProvider)(nil)
}
