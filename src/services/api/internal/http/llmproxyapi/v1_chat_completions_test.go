package llmproxyapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arkloop/services/shared/acptoken"
)

const testSecret = "test-secret-for-llm-proxy-testing-min-32-bytes"

func testValidator(t *testing.T) *acptoken.Validator {
	t.Helper()
	v, err := acptoken.NewValidator(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func issueTestToken(t *testing.T, models []string) string {
	t.Helper()
	iss, err := acptoken.NewIssuer(testSecret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := iss.Issue(acptoken.IssueParams{
		RunID:     "test-run-id",
		AccountID: "test-account-id",
		Models:    models,
		Budget:    0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestMissingToken(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/llm-proxy/chat/completions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestInvalidToken(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/llm-proxy/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer invalid-garbage-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestModelNotAllowed(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	token := issueTestToken(t, []string{"claude-sonnet-4-5"})

	body, _ := json.Marshal(chatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/llm-proxy/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestMissingModel(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	token := issueTestToken(t, nil)

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role":    "user",
			"content": "hello",
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/llm-proxy/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/llm-proxy/chat/completions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestValidTokenWithAllowedModel(t *testing.T) {
	handler := chatCompletionsEntry(Deps{
		TokenValidator: testValidator(t),
	})

	token := issueTestToken(t, []string{"claude-sonnet-4-5"})

	body, _ := json.Marshal(chatCompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/llm-proxy/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should get 502 (no upstream configured) rather than 401/403
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (bad gateway because no upstream)", w.Code, http.StatusBadGateway)
	}
}
