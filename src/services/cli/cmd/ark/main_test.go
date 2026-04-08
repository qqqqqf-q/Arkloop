package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/cli/internal/apiclient"
)

func TestResolveHostUsesExplicitFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got := resolveHost("http://127.0.0.1:29999", true)
	if got != "http://127.0.0.1:29999" {
		t.Fatalf("expected explicit host, got %q", got)
	}
}

func TestResolveHostUsesEnvVar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ARKLOOP_HOST", "http://127.0.0.1:29998")

	got := resolveHost("", false)
	if got != "http://127.0.0.1:29998" {
		t.Fatalf("expected env host, got %q", got)
	}
}

func TestResolveHostUsesDesktopConfigPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".arkloop")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	config := `{"mode":"local","local":{"port":19035,"portMode":"auto"}}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := resolveHost("", false)
	if got != "http://127.0.0.1:19035" {
		t.Fatalf("expected config host, got %q", got)
	}
}

func TestResolveHostFallsBackWhenConfigInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".arkloop")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"mode":"local","local":{"port":99999}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := resolveHost("", false)
	if got != "http://127.0.0.1:19001" {
		t.Fatalf("expected default host, got %q", got)
	}
}

func TestResolveHostIgnoresNonLocalMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".arkloop")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"mode":"saas","saas":{"baseUrl":"https://api.example.com"},"local":{"port":19035}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := resolveHost("", false)
	if got != "http://127.0.0.1:19001" {
		t.Fatalf("expected default host for non-local mode, got %q", got)
	}
}

func TestResolveTokenUsesEnvVar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ARKLOOP_TOKEN", "env-token")

	got := resolveToken("")
	if got != "env-token" {
		t.Fatalf("expected env token, got %q", got)
	}
}

func TestSplitFlagAndPositionalArgsAllowsPositionalBeforeFlags(t *testing.T) {
	valueFlags := map[string]struct{}{
		"model": {},
	}
	flagArgs, positionals, err := splitFlagAndPositionalArgs([]string{"session-1", "--model", "gpt-4.1"}, valueFlags)
	if err != nil {
		t.Fatalf("splitFlagAndPositionalArgs: %v", err)
	}
	if strings.Join(flagArgs, " ") != "--model gpt-4.1" {
		t.Fatalf("unexpected flag args: %#v", flagArgs)
	}
	if len(positionals) != 1 || positionals[0] != "session-1" {
		t.Fatalf("unexpected positionals: %#v", positionals)
	}
}

func TestLoadPromptFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(path, []byte("from-file"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	got, err := loadPrompt("", path, strings.NewReader(""), false)
	if err != nil {
		t.Fatalf("loadPrompt: %v", err)
	}
	if got != "from-file" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestLoadPromptFromStdin(t *testing.T) {
	got, err := loadPrompt("-", "", strings.NewReader("from-stdin"), false)
	if err != nil {
		t.Fatalf("loadPrompt: %v", err)
	}
	if got != "from-stdin" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestLoadPromptRejectsPromptAndFileTogether(t *testing.T) {
	_, err := loadPrompt("inline", "prompt.txt", strings.NewReader(""), false)
	if !errors.Is(err, errRunUsage) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestLoadPromptAllowsImplicitStdin(t *testing.T) {
	got, err := loadPrompt("", "", strings.NewReader("implicit-stdin"), true)
	if err != nil {
		t.Fatalf("loadPrompt: %v", err)
	}
	if got != "implicit-stdin" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestRunJSONWritesStructuredErrorOnPreRunFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"auth.invalid_token","message":"token invalid"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := apiclient.NewClient(server.URL, "bad-token")
	out := &bytes.Buffer{}
	err := runJSON(context.Background(), out, client, "", "hello", apiclient.RunParams{})
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("expected exitError code 1, got %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"is_error":true`) || !strings.Contains(body, `"status":"error"`) {
		t.Fatalf("unexpected json output: %s", body)
	}
	if !strings.Contains(body, `"error":"runner: create thread:`) {
		t.Fatalf("expected pre-run error in json output: %s", body)
	}
}
