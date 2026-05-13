package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, "test-token")
}

func TestGetMe(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","username":"qq","account_id":"a1","work_enabled":true}`))
	})

	got, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if got.ID != "u1" || got.Username != "qq" || got.AccountID != "a1" || !got.WorkEnabled {
		t.Fatalf("unexpected me: %#v", got)
	}
}

func TestListSelectablePersonas(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/selectable-personas" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"p1","persona_key":"search","display_name":"Search","selector_name":"Search","model":"gpt-4.1","reasoning_mode":"enabled","source":"builtin"}]`))
	})

	got, err := client.ListSelectablePersonas(context.Background())
	if err != nil {
		t.Fatalf("ListSelectablePersonas: %v", err)
	}
	if len(got) != 1 || got[0].PersonaKey != "search" {
		t.Fatalf("unexpected personas: %#v", got)
	}
}

func TestListLlmProviders(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v1/llm-providers?scope=user" {
			t.Fatalf("unexpected uri: %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"provider-1","name":"OpenAI","models":[{"id":"m1","provider_id":"provider-1","model":"gpt-4.1","is_default":true,"show_in_picker":true,"tags":["chat"]}]}]`))
	})

	got, err := client.ListLlmProviders(context.Background())
	if err != nil {
		t.Fatalf("ListLlmProviders: %v", err)
	}
	if len(got) != 1 || got[0].Name != "OpenAI" || len(got[0].Models) != 1 {
		t.Fatalf("unexpected providers: %#v", got)
	}
}

func TestListThreads(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v1/threads?limit=200" {
			t.Fatalf("unexpected uri: %s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"t1","mode":"chat","title":"Hello","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z","active_run_id":"r1","is_private":false}]`))
	})

	got, err := client.ListThreads(context.Background(), 200)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(got) != 1 || got[0].ID != "t1" || got[0].UpdatedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("unexpected threads: %#v", got)
	}
}

func TestListThreadsBefore(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/v1/threads" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if query.Get("limit") != "200" {
			t.Fatalf("unexpected limit: %s", query.Get("limit"))
		}
		if query.Get("before_created_at") != "2026-01-02T00:00:00Z" {
			t.Fatalf("unexpected before_created_at: %s", query.Get("before_created_at"))
		}
		if query.Get("before_id") != "t9" {
			t.Fatalf("unexpected before_id: %s", query.Get("before_id"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})

	got, err := client.ListThreadsBefore(context.Background(), 200, "2026-01-02T00:00:00Z", "t9")
	if err != nil {
		t.Fatalf("ListThreadsBefore: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unexpected threads: %#v", got)
	}
}

func TestGetRun(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/r1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"r1","thread_id":"t1","status":"running"}`))
	})

	got, err := client.GetRun(context.Background(), "r1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.RunID != "r1" || got.ThreadID != "t1" || got.Status != "running" {
		t.Fatalf("unexpected run: %#v", got)
	}
}

func TestGetMeReturnsHTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"auth.invalid_token","message":"token invalid"}`, http.StatusUnauthorized)
	})

	if _, err := client.GetMe(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestListAllThreadsPaginates(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")

		if query.Get("before_id") == "" {
			payload := make([]string, 0, ThreadPageLimit)
			for i := 0; i < ThreadPageLimit; i++ {
				payload = append(payload, fmt.Sprintf(`{"id":"t%03d","mode":"chat","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","is_private":false}`, ThreadPageLimit-i))
			}
			_, _ = w.Write([]byte("[" + strings.Join(payload, ",") + "]"))
			return
		}
		if query.Get("before_id") != "t001" || query.Get("before_created_at") != "2026-01-01T00:00:00Z" {
			t.Fatalf("unexpected cursor query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"id":"t000","mode":"chat","created_at":"2025-12-31T00:00:00Z","updated_at":"2025-12-31T00:00:00Z","is_private":false}]`))
	})

	got, err := client.ListAllThreads(context.Background())
	if err != nil {
		t.Fatalf("ListAllThreads: %v", err)
	}
	if len(got) != ThreadPageLimit+1 {
		t.Fatalf("unexpected thread count: %d", len(got))
	}
	if got[0].ID != "t200" || got[len(got)-1].ID != "t000" {
		t.Fatalf("unexpected threads: %#v", got)
	}
}

func TestListAllThreadsRejectsIncompleteCursor(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := make([]string, 0, ThreadPageLimit)
		for i := 0; i < ThreadPageLimit; i++ {
			payload = append(payload, `{"id":"","mode":"chat","created_at":"","updated_at":"2026-01-01T00:00:00Z","is_private":false}`)
		}
		_, _ = w.Write([]byte("[" + strings.Join(payload, ",") + "]"))
	})

	if _, err := client.ListAllThreads(context.Background()); err == nil {
		t.Fatal("expected pagination cursor error")
	}
}

func TestInstallPluginSendsManifestPath(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/plugins" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["manifest_path"] != "/tmp/plugin.json" {
			t.Fatalf("unexpected body: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"demo.plugin","version":"1.0.0","display_name":"Demo","source_kind":"manifest","is_active":true}`))
	})

	got, err := client.InstallPlugin(context.Background(), PluginInstallRequest{ManifestPath: "/tmp/plugin.json"})
	if err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	if got.ID != "demo.plugin" || got.Version != "1.0.0" {
		t.Fatalf("unexpected plugin: %#v", got)
	}
}

func TestSetPluginEnablementRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/plugins/demo.plugin/enablements" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["enabled"] != true || body["workspace_ref"] != "wsref_1" {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plugin_id":"demo.plugin","plugin_version":"1.0.0","workspace_ref":"wsref_1","enabled":true,"settings":{}}`))
	})

	got, err := client.SetPluginEnablement(context.Background(), "demo.plugin", PluginEnablementRequest{
		WorkspaceRef: "wsref_1",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("SetPluginEnablement: %v", err)
	}
	if got.PluginID != "demo.plugin" || !got.Enabled {
		t.Fatalf("unexpected enablement: %#v", got)
	}
}
