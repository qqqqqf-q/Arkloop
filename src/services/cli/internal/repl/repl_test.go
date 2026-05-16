package repl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/cli/internal/apiclient"
)

func newTestREPL(t *testing.T, handler http.HandlerFunc) (*REPL, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := apiclient.NewClient(server.URL, "test-token")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return newREPLWithWriters(client, apiclient.RunParams{}, "", 30*time.Second, false, stdout, stderr), stderr
}

func TestHandleHelp(t *testing.T) {
	repl, stderr := newTestREPL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handled, err := repl.handleCommand(context.Background(), "/help")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || !strings.Contains(stderr.String(), "/model <name>") {
		t.Fatalf("unexpected help output: %s", stderr.String())
	}
}

func TestHandleStatus(t *testing.T) {
	repl, stderr := newTestREPL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	repl.timeout = 0

	handled, err := repl.handleCommand(context.Background(), "/status")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || !strings.Contains(stderr.String(), "session id: new") || !strings.Contains(stderr.String(), "timeout: none") {
		t.Fatalf("unexpected status output: %s", stderr.String())
	}
}

func TestHandleModelUpdatesState(t *testing.T) {
	repl, stderr := newTestREPL(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v1/llm-providers?scope=user" {
			t.Fatalf("unexpected uri: %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"provider-1","name":"OpenAI","models":[{"id":"m1","provider_id":"provider-1","model":"gpt-4.1","is_default":true,"show_in_picker":true,"tags":[]}]}]`))
	})

	handled, err := repl.handleCommand(context.Background(), "/model gpt-4.1")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || repl.params.Model != "gpt-4.1" || !strings.Contains(stderr.String(), "model: gpt-4.1") {
		t.Fatalf("unexpected model state: %#v, stderr=%s", repl.params, stderr.String())
	}
}

func TestHandlePersonaUpdatesState(t *testing.T) {
	repl, stderr := newTestREPL(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/selectable-personas" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"p1","persona_key":"search","display_name":"Search","selector_name":"Search","model":"gpt-4.1","reasoning_mode":"enabled","source":"builtin"}]`))
	})

	handled, err := repl.handleCommand(context.Background(), "/persona search")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || repl.params.PersonaID != "search" || !strings.Contains(stderr.String(), "persona: search") {
		t.Fatalf("unexpected persona state: %#v, stderr=%s", repl.params, stderr.String())
	}
}

func TestHandleNewResetsSession(t *testing.T) {
	repl, stderr := newTestREPL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	repl.threadID = "thread-1"

	handled, err := repl.handleCommand(context.Background(), "/new")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || repl.threadID != "" || !strings.Contains(stderr.String(), "new session") {
		t.Fatalf("unexpected new output: %s", stderr.String())
	}
}

func TestHandleQuitReturnsEOF(t *testing.T) {
	repl, _ := newTestREPL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handled, err := repl.handleCommand(context.Background(), "/quit")
	if !handled || !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got handled=%t err=%v", handled, err)
	}
}
