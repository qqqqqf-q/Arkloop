//go:build !desktop

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzIsFastPath(t *testing.T) {
	called := 0
	full := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})

	root := http.NewServeMux()
	root.HandleFunc("/healthz", healthz)
	root.Handle("/", full)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("unexpected payload: %+v", resp)
	}
	if called != 0 {
		t.Fatalf("full handler should not be called, got %d", called)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec2 := httptest.NewRecorder()
	root.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec2.Code)
	}
	if called != 1 {
		t.Fatalf("full handler should be called once, got %d", called)
	}
}
