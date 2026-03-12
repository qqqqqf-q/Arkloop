package sandbox

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestCleanupSession_DeletesRunAndShellSessions(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Account-ID"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	CleanupSession(server.URL, "", "run-123", "org-123")

	sort.Strings(requests)
	expected := []string{
		"DELETE /v1/sessions/run-123 org-123",
		"DELETE /v1/sessions/run-123/shell/default org-123",
	}
	if len(requests) != len(expected) {
		t.Fatalf("expected %d requests, got %d: %#v", len(expected), len(requests), requests)
	}
	for i := range expected {
		if requests[i] != expected[i] {
			t.Fatalf("unexpected request[%d]: %s", i, requests[i])
		}
	}
}
