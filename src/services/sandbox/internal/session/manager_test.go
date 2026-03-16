package session

import (
	"context"
	"testing"
)

type stubPool struct {
	tier string
}

func (p *stubPool) Acquire(_ context.Context, sessionID, tier string) (*Session, error) {
	p.tier = tier
	return &Session{ID: sessionID, Tier: tier}, nil
}

func (p *stubPool) Destroy(_ string)           {}
func (p *stubPool) Ready() bool                { return true }
func (p *stubPool) Stats() PoolStats           { return PoolStats{} }
func (p *stubPool) Drain(_ context.Context)    {}

func TestValidTierAcceptsBrowser(t *testing.T) {
	if err := ValidTier(TierBrowser); err != nil {
		t.Fatalf("browser tier should be valid: %v", err)
	}
}

func TestManagerAppliesBrowserLifecycleByTier(t *testing.T) {
	pool := &stubPool{}
	mgr := NewManager(ManagerConfig{
		MaxSessions: 1,
		Pool:        pool,
		IdleTimeouts: map[string]int{
			TierLite:    180,
			TierPro:     300,
			TierBrowser: 120,
		},
		MaxLifetimes: map[string]int{
			TierLite:    1800,
			TierPro:     1800,
			TierBrowser: 600,
		},
	})

	sn, err := mgr.GetOrCreate(context.Background(), "browser-session", TierBrowser, "org-a")
	if err != nil {
		t.Fatalf("get or create browser session: %v", err)
	}
	if pool.tier != TierBrowser {
		t.Fatalf("unexpected acquired tier: %s", pool.tier)
	}
	if got := int(sn.IdleTimeout.Seconds()); got != 120 {
		t.Fatalf("unexpected browser idle timeout: %d", got)
	}
	if got := int(sn.MaxLifetime.Seconds()); got != 600 {
		t.Fatalf("unexpected browser max lifetime: %d", got)
	}
}
