package toolmeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxToolDescriptionsExplainWorkspaceAndArtifacts(t *testing.T) {
	python := Must("python_execute").LLMDescription
	if !strings.Contains(python, "/workspace/") || !strings.Contains(python, "/tmp/output/") {
		t.Fatalf("python_execute description should mention /workspace/ and /tmp/output/: %s", python)
	}

	execDesc := Must("exec_command").LLMDescription
	if !strings.Contains(execDesc, "/workspace/") || !strings.Contains(execDesc, "/tmp/output/") {
		t.Fatalf("exec_command description should mention /workspace/ and /tmp/output/: %s", execDesc)
	}

	stdinDesc := Must("write_stdin").LLMDescription
	if !strings.Contains(stdinDesc, "session_ref") || !strings.Contains(stdinDesc, "/workspace") {
		t.Fatalf("write_stdin description should mention session_ref and /workspace: %s", stdinDesc)
	}

	browserDesc := Must("browser").LLMDescription
	if !strings.Contains(browserDesc, "running=true") || !strings.Contains(browserDesc, "yield_time_ms") {
		t.Fatalf("browser description should explain running=true and yield_time_ms: %s", browserDesc)
	}
	if !strings.Contains(browserDesc, "not a mode flag") {
		t.Fatalf("browser description should explain session_ref semantics: %s", browserDesc)
	}

	for _, desc := range []string{python, execDesc, stdinDesc} {
		if !strings.Contains(desc, "workspace:/relative/path") {
			t.Fatalf("sandbox tool description should mention workspace protocol: %s", desc)
		}
		if !strings.Contains(desc, "Never invent artifact keys") {
			t.Fatalf("sandbox tool description should forbid invented artifact keys: %s", desc)
		}
	}
}

func TestSearchOutputPromptExplainsWorkspaceAndArtifactRules(t *testing.T) {
	promptPath := filepath.Join("..", "..", "..", "personas", "search-output", "prompt.md")
	body, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "workspace:/path") {
		t.Fatalf("prompt should mention workspace protocol: %s", content)
	}
	if !strings.Contains(content, "禁止根据 stdout、stderr、本地路径或文件名臆造新的 `artifact:<key>` 或 `workspace:/path`") {
		t.Fatalf("prompt should forbid invented file references: %s", content)
	}
}
