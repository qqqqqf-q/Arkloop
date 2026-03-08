package http

import (
	"bytes"
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/sandbox/internal/shell"
)

type stubShellService struct {
	debugFn func(ctx context.Context, sessionID, orgID string) (*shell.DebugResponse, error)
	forkFn  func(ctx context.Context, req shell.ForkSessionRequest) (*shell.ForkSessionResponse, error)
	closeFn func(ctx context.Context, sessionID, orgID string) error
}

func (s *stubShellService) ExecCommand(context.Context, shell.ExecCommandRequest) (*shell.Response, error) {
	return nil, nil
}

func (s *stubShellService) WriteStdin(context.Context, shell.WriteStdinRequest) (*shell.Response, error) {
	return nil, nil
}

func (s *stubShellService) DebugSnapshot(ctx context.Context, sessionID, orgID string) (*shell.DebugResponse, error) {
	if s.debugFn != nil {
		return s.debugFn(ctx, sessionID, orgID)
	}
	return &shell.DebugResponse{SessionID: sessionID, Status: shell.StatusIdle}, nil
}

func (s *stubShellService) ForkSession(ctx context.Context, req shell.ForkSessionRequest) (*shell.ForkSessionResponse, error) {
	if s.forkFn != nil {
		return s.forkFn(ctx, req)
	}
	return &shell.ForkSessionResponse{}, nil
}

func (s *stubShellService) Close(ctx context.Context, sessionID, orgID string) error {
	if s.closeFn != nil {
		return s.closeFn(ctx, sessionID, orgID)
	}
	return nil
}

func TestHandleSessionTranscript_OK(t *testing.T) {
	handler := handleSessionTranscript(&stubShellService{debugFn: func(_ context.Context, sessionID, orgID string) (*shell.DebugResponse, error) {
		if sessionID != "sess-1" {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
		if orgID != "org-a" {
			t.Fatalf("unexpected org id: %s", orgID)
		}
		code := 0
		return &shell.DebugResponse{
			SessionID:              sessionID,
			Status:                 shell.StatusIdle,
			Cwd:                    "/workspace",
			Running:                false,
			TimedOut:               false,
			ExitCode:               &code,
			PendingOutputBytes:     10,
			PendingOutputTruncated: true,
			Transcript:             shell.DebugTranscript{Text: "hello", Truncated: true, OmittedBytes: 12},
			Tail:                   "llo",
		}, nil
	}})

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/sessions/sess-1/transcript", nil)
	req.Header.Set("X-Org-ID", "org-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp shell.DebugResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionID != "sess-1" || resp.Transcript.Text != "hello" || resp.Tail != "llo" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleForkSession_OK(t *testing.T) {
	handler := handleForkSession(&stubShellService{forkFn: func(_ context.Context, req shell.ForkSessionRequest) (*shell.ForkSessionResponse, error) {
		if req.FromSessionID != "shref_a" || req.ToSessionID != "shref_b" {
			t.Fatalf("unexpected fork request: %#v", req)
		}
		if req.OrgID != "org-a" {
			t.Fatalf("unexpected org id: %s", req.OrgID)
		}
		return &shell.ForkSessionResponse{RestoreRevision: "rev-1"}, nil
	}})
	body, _ := json.Marshal(map[string]any{"from_session_id": "shref_a", "to_session_id": "shref_b"})
	req := httptest.NewRequest(nethttp.MethodPost, "/v1/sessions/fork", bytes.NewReader(body))
	req.Header.Set("X-Org-ID", "org-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp shell.ForkSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RestoreRevision != "rev-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleSessionTranscript_NotFound(t *testing.T) {
	handler := handleSessionTranscript(&stubShellService{debugFn: func(_ context.Context, _, _ string) (*shell.DebugResponse, error) {
		return nil, &shell.Error{Code: shell.CodeSessionNotFound, Message: "shell session not found", HTTPStatus: nethttp.StatusNotFound}
	}})

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/sessions/missing/transcript", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleSessionTranscript_OrgMismatch(t *testing.T) {
	handler := handleSessionTranscript(&stubShellService{debugFn: func(_ context.Context, _, _ string) (*shell.DebugResponse, error) {
		return nil, &shell.Error{Code: shell.CodeOrgMismatch, Message: "session belongs to another org", HTTPStatus: nethttp.StatusForbidden}
	}})

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/sessions/sess-1/transcript", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != nethttp.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
