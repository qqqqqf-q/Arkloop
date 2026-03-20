package plugin

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// -- mock implementations --

type mockBillingProvider struct{ id string }

func (m *mockBillingProvider) CreateSubscription(context.Context, uuid.UUID, string) error {
	return nil
}
func (m *mockBillingProvider) CancelSubscription(context.Context, uuid.UUID) error { return nil }
func (m *mockBillingProvider) GetActiveSubscription(context.Context, uuid.UUID) (*Subscription, error) {
	return nil, nil
}
func (m *mockBillingProvider) ReportUsage(context.Context, uuid.UUID, UsageRecord) error { return nil }
func (m *mockBillingProvider) CheckQuota(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}
func (m *mockBillingProvider) HandleWebhook(context.Context, string, []byte, string) error {
	return nil
}

type mockAuthProvider struct{ name string }

func (m *mockAuthProvider) Name() string { return m.name }
func (m *mockAuthProvider) AuthCodeURL(context.Context, string) (string, error) {
	return "", nil
}
func (m *mockAuthProvider) ExchangeToken(context.Context, string) (*ExternalIdentity, error) {
	return nil, nil
}
func (m *mockAuthProvider) RefreshExternalToken(context.Context, string) (*ExternalIdentity, error) {
	return nil, nil
}

type mockNotificationChannel struct{ name string }

func (m *mockNotificationChannel) Name() string { return m.name }
func (m *mockNotificationChannel) Send(context.Context, Notification) (string, error) {
	return "", nil
}

type mockAuditSink struct{ name string }

func (m *mockAuditSink) Name() string { return m.name }
func (m *mockAuditSink) Emit(context.Context, AuditEvent) error {
	return nil
}

// -- tests --

func TestRegisterAndGetBillingProvider(t *testing.T) {
	resetForTesting()

	if got := GetBillingProvider(); got != nil {
		t.Fatal("expected nil before registration")
	}

	p := &mockBillingProvider{id: "stripe"}
	RegisterBillingProvider(p)

	got := GetBillingProvider()
	if got == nil {
		t.Fatal("expected non-nil after registration")
	}
	if got.(*mockBillingProvider).id != "stripe" {
		t.Fatalf("got id %q, want %q", got.(*mockBillingProvider).id, "stripe")
	}
}

func TestRegisterBillingProviderOverride(t *testing.T) {
	resetForTesting()

	RegisterBillingProvider(&mockBillingProvider{id: "first"})
	RegisterBillingProvider(&mockBillingProvider{id: "second"})

	got := GetBillingProvider()
	if got.(*mockBillingProvider).id != "second" {
		t.Fatalf("override failed: got %q, want %q", got.(*mockBillingProvider).id, "second")
	}
}

func TestRegisterAndGetAuthProvider(t *testing.T) {
	resetForTesting()

	RegisterAuthProvider("oidc", &mockAuthProvider{name: "oidc"})
	RegisterAuthProvider("saml", &mockAuthProvider{name: "saml"})

	p, ok := GetAuthProvider("oidc")
	if !ok || p.Name() != "oidc" {
		t.Fatalf("expected oidc provider, got ok=%v name=%v", ok, p)
	}

	p, ok = GetAuthProvider("saml")
	if !ok || p.Name() != "saml" {
		t.Fatalf("expected saml provider, got ok=%v name=%v", ok, p)
	}

	_, ok = GetAuthProvider("nonexistent")
	if ok {
		t.Fatal("expected false for nonexistent provider")
	}
}

func TestRegisterAndGetNotificationChannels(t *testing.T) {
	resetForTesting()

	RegisterNotificationChannel("slack", &mockNotificationChannel{name: "slack"})
	RegisterNotificationChannel("email", &mockNotificationChannel{name: "email"})

	channels := ListNotificationChannels()
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
	if channels["slack"].Name() != "slack" {
		t.Fatal("slack channel missing or wrong")
	}
	if channels["email"].Name() != "email" {
		t.Fatal("email channel missing or wrong")
	}

	// 修改返回副本不影响内部状态
	channels["webhook"] = &mockNotificationChannel{name: "webhook"}
	delete(channels, "slack")

	fresh := ListNotificationChannels()
	if len(fresh) != 2 {
		t.Fatalf("internal state corrupted: expected 2, got %d", len(fresh))
	}
	if _, ok := fresh["slack"]; !ok {
		t.Fatal("internal state corrupted: slack missing")
	}
	if _, ok := fresh["webhook"]; ok {
		t.Fatal("internal state corrupted: webhook should not exist")
	}
}

func TestRegisterAndGetAuditSinks(t *testing.T) {
	resetForTesting()

	RegisterAuditSink(&mockAuditSink{name: "stdout"})
	RegisterAuditSink(&mockAuditSink{name: "s3"})

	sinks := GetAuditSinks()
	if len(sinks) != 2 {
		t.Fatalf("expected 2 sinks, got %d", len(sinks))
	}
	if sinks[0].Name() != "stdout" || sinks[1].Name() != "s3" {
		t.Fatalf("unexpected sink order: %v, %v", sinks[0].Name(), sinks[1].Name())
	}

	// 修改返回副本不影响内部状态
	sinks[0] = &mockAuditSink{name: "replaced"}
	_ = append(sinks, &mockAuditSink{name: "extra"})

	fresh := GetAuditSinks()
	if len(fresh) != 2 {
		t.Fatalf("internal state corrupted: expected 2, got %d", len(fresh))
	}
	if fresh[0].Name() != "stdout" {
		t.Fatalf("internal state corrupted: first sink is %q, want %q", fresh[0].Name(), "stdout")
	}
}

func TestRegistryIsolation(t *testing.T) {
	// 先污染注册表
	RegisterBillingProvider(&mockBillingProvider{id: "leftover"})
	RegisterAuthProvider("leftover", &mockAuthProvider{name: "leftover"})
	RegisterNotificationChannel("leftover", &mockNotificationChannel{name: "leftover"})
	RegisterAuditSink(&mockAuditSink{name: "leftover"})

	resetForTesting()

	if GetBillingProvider() != nil {
		t.Fatal("billing not reset")
	}
	if _, ok := GetAuthProvider("leftover"); ok {
		t.Fatal("auth not reset")
	}
	if len(ListNotificationChannels()) != 0 {
		t.Fatal("notification channels not reset")
	}
	if len(GetAuditSinks()) != 0 {
		t.Fatal("audit sinks not reset")
	}
}
