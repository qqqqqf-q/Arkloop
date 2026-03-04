package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

func TestExecOrgBinding_FirstRequest(t *testing.T) {
	mgr := newTestManager()

	s1, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.OrgID != "org-a" {
		t.Errorf("expected OrgID=org-a, got %s", s1.OrgID)
	}
}

func TestExecOrgBinding_SameOrg(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	s2, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("same org: %v", err)
	}
	if s2.OrgID != "org-a" {
		t.Errorf("expected OrgID=org-a, got %s", s2.OrgID)
	}
}

func TestExecOrgBinding_DifferentOrg(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-b")
	if err == nil {
		t.Fatal("expected org mismatch error")
	}
}

func TestExecOrgBinding_EmptyOrgSkipsCheck(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = mgr.GetOrCreate(t.Context(), "sess-1", "lite", "")
	if err != nil {
		t.Fatalf("empty org should be allowed: %v", err)
	}
}

func TestDeleteOrgBinding_Mismatch(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = mgr.Delete(t.Context(), "sess-1", "org-b")
	if err == nil {
		t.Fatal("expected org mismatch error on delete")
	}
}

func TestDeleteOrgBinding_Match(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "sess-1", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = mgr.Delete(t.Context(), "sess-1", "org-a")
	if err != nil {
		t.Fatalf("delete same org should succeed: %v", err)
	}
}

func TestExecHandler_OrgMismatch_Returns403(t *testing.T) {
	mgr := newTestManager()

	_, err := mgr.GetOrCreate(t.Context(), "test-session", "lite", "org-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	handler := handleExec(mgr, nil, logging.NewJSONLogger("test", nil))

	body, _ := json.Marshal(ExecRequest{
		SessionID: "test-session",
		OrgID:     "org-b",
		Tier:      "lite",
		Language:  "python",
		Code:      "print(1)",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/exec", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func newTestManager() *session.Manager {
	return session.NewManager(session.ManagerConfig{
		MaxSessions:        100,
		Pool:               &noopPool{},
		IdleTimeoutLite:    0,
		IdleTimeoutPro:     0,
		IdleTimeoutUltra:   0,
		MaxLifetimeSeconds: 3600,
	})
}

// noopPool 是测试用的 VMPool 实现，不创建真实 VM。
type noopPool struct{}

func (p *noopPool) Acquire(_ context.Context, tier string) (*session.Session, *os.Process, error) {
	return &session.Session{Tier: tier}, nil, nil
}

func (p *noopPool) DestroyVM(_ *os.Process, _ string) {}
func (p *noopPool) Ready() bool                       { return true }
func (p *noopPool) Stats() session.PoolStats          { return session.PoolStats{} }
func (p *noopPool) Drain(_ context.Context)            {}
