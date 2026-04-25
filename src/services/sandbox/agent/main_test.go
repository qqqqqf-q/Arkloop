package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	environmentcontract "arkloop/services/sandbox/internal/environment/contract"
)

func TestLimitedBuffer_Truncates(t *testing.T) {
	buf := newLimitedBuffer(10)
	n, err := buf.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len("hello world") {
		t.Fatalf("expected n=%d, got %d", len("hello world"), n)
	}
	if got := buf.String(); got != "hello worl" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestExecuteJob_CodeTooLarge(t *testing.T) {
	job := ExecJob{
		Language:  "python",
		Code:      strings.Repeat("a", maxCodeBytes+1),
		TimeoutMs: 1000,
	}
	result := executeJob(job)
	if result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stderr) == "" {
		t.Fatalf("expected stderr not empty")
	}
}

func TestFetchArtifactsFromDir_UsesContentType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preview.png")
	content := []byte("<!doctype html><html><body>preview</body></html>")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	result := fetchArtifactsFromDir(dir)
	if len(result.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(result.Artifacts))
	}
	if got := result.Artifacts[0].MimeType; got != "text/html" {
		t.Fatalf("mime type = %q, want text/html", got)
	}
}

func TestDetectMimeType_SVG(t *testing.T) {
	data := []byte("<?xml version=\"1.0\"?><svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")
	if got := detectMimeType(data); got != "image/svg+xml" {
		t.Fatalf("mime type = %q, want image/svg+xml", got)
	}
}

func TestDetectMimeType_FallbackToOctetStream(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "unknown_binary", data: []byte{0x00, 0x9f, 0x92, 0x96}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectMimeType(tc.data); got != "application/octet-stream" {
				t.Fatalf("mime type = %q, want application/octet-stream", got)
			}
		})
	}
}

func TestHandleV2AgentCapabilities(t *testing.T) {
	resp := invokeAgentRequest(t, AgentRequest{Action: "agent_capabilities"})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Capabilities == nil {
		t.Fatal("expected capabilities payload")
	}
	if resp.Capabilities.ProtocolVersion != agentProtocolVersion {
		t.Fatalf("unexpected protocol version: %d", resp.Capabilities.ProtocolVersion)
	}
	for _, action := range []string{"environment_manifest_build", "environment_files_collect", "environment_apply"} {
		if !containsString(resp.Capabilities.EnvironmentActions, action) {
			t.Fatalf("missing action %s in %v", action, resp.Capabilities.EnvironmentActions)
		}
	}
}

func TestHandleV2EnvironmentManifestBuild(t *testing.T) {
	workspaceDir := t.TempDir()
	homeDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	oldHome := shellHomeDir
	shellWorkspaceDir = workspaceDir
	shellHomeDir = homeDir
	defer func() {
		shellWorkspaceDir = oldWorkspace
		shellHomeDir = oldHome
	}()

	if err := os.WriteFile(filepath.Join(workspaceDir, "chart.png"), []byte("png-data"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	resp := invokeAgentRequest(t, AgentRequest{Action: "environment_manifest_build", Environment: &EnvironmentRequest{Scope: "workspace"}})
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Environment == nil || resp.Environment.Manifest == nil {
		t.Fatal("expected manifest response")
	}
	entries := resp.Environment.Manifest.Entries
	if len(entries) != 1 || entries[0].Path != "chart.png" || entries[0].Type != "file" {
		t.Fatalf("unexpected manifest entries: %#v", entries)
	}
}

func TestHandleV2EnvironmentFilesCollectAndApply(t *testing.T) {
	workspaceDir := t.TempDir()
	homeDir := t.TempDir()
	oldWorkspace := shellWorkspaceDir
	oldHome := shellHomeDir
	shellWorkspaceDir = workspaceDir
	shellHomeDir = homeDir
	defer func() {
		shellWorkspaceDir = oldWorkspace
		shellHomeDir = oldHome
	}()

	if err := os.WriteFile(filepath.Join(workspaceDir, "chart.png"), []byte("png-data"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	collectResp := invokeAgentRequest(t, AgentRequest{Action: "environment_files_collect", Environment: &EnvironmentRequest{Scope: "workspace", Paths: []string{"chart.png"}}})
	if collectResp.Error != "" {
		t.Fatalf("unexpected collect error: %s", collectResp.Error)
	}
	if collectResp.Environment == nil || len(collectResp.Environment.Files) != 1 {
		t.Fatalf("unexpected collect response: %#v", collectResp.Environment)
	}
	if err := os.Remove(filepath.Join(workspaceDir, "chart.png")); err != nil {
		t.Fatalf("remove workspace file: %v", err)
	}
	applyResp := invokeAgentRequest(t, AgentRequest{Action: "environment_apply", Environment: &EnvironmentRequest{
		Scope:             "workspace",
		Manifest:          &environmentcontract.Manifest{Scope: "workspace", Entries: []environmentcontract.ManifestEntry{{Path: "chart.png", Type: "file", Mode: 0o644, Size: 8, SHA256: collectResp.Environment.Files[0].SHA256}}},
		Files:             collectResp.Environment.Files,
		PruneRootChildren: true,
	}})
	if applyResp.Error != "" {
		t.Fatalf("unexpected apply error: %s", applyResp.Error)
	}
	data, err := os.ReadFile(filepath.Join(workspaceDir, "chart.png"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "png-data" {
		t.Fatalf("unexpected restored data: %q", data)
	}
}

func invokeAgentRequest(t *testing.T, req AgentRequest) AgentResponse {
	t.Helper()
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	go handleV2(server, req)
	var resp AgentResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
