package toolmeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxToolDescriptionsPreferArtifactsAndAbsoluteFilePaths(t *testing.T) {
	python := Must("python_execute").LLMDescription
	if !strings.Contains(python, "/workspace/") || !strings.Contains(python, "/tmp/output/") {
		t.Fatalf("python_execute description should mention /workspace/ and /tmp/output/: %s", python)
	}
	if !strings.Contains(python, "exact absolute file_path") {
		t.Fatalf("python_execute description should prefer absolute file paths: %s", python)
	}

	execDesc := Must("exec_command").LLMDescription
	if !strings.Contains(execDesc, "/workspace/") || !strings.Contains(execDesc, "/tmp/output/") {
		t.Fatalf("exec_command description should mention /workspace/ and /tmp/output/: %s", execDesc)
	}
	if !strings.Contains(execDesc, "exact absolute file_path") {
		t.Fatalf("exec_command description should prefer absolute file paths: %s", execDesc)
	}
	if !strings.Contains(execDesc, "browser:<http-or-https-url>") || !strings.Contains(execDesc, "browser:http://localhost:5173") {
		t.Fatalf("exec_command description should explain browser resource links: %s", execDesc)
	}

	continueDesc := Must("continue_process").LLMDescription
	if !strings.Contains(continueDesc, "process_ref") || !strings.Contains(continueDesc, "exact absolute file_path") {
		t.Fatalf("continue_process description should mention process_ref and absolute file paths: %s", continueDesc)
	}

	browserDesc := Must("browser").LLMDescription
	if strings.Contains(browserDesc, "running=true") || !strings.Contains(browserDesc, "yield_time_ms") {
		t.Fatalf("browser description should hide running=true and explain yield_time_ms: %s", browserDesc)
	}
	if strings.Contains(browserDesc, "session_ref") || !strings.Contains(browserDesc, "backend") {
		t.Fatalf("browser description should hide session_ref and explain backend session handling: %s", browserDesc)
	}
	if !strings.Contains(browserDesc, "Snapshot results are compact by default") || !strings.Contains(browserDesc, "Use screenshot only when you need a visual image") {
		t.Fatalf("browser description should explain compact snapshot and screenshot usage: %s", browserDesc)
	}
	if !strings.Contains(browserDesc, "avoid tiny values such as 50ms") || !strings.Contains(browserDesc, "1500-5000ms") {
		t.Fatalf("browser description should guide practical yield_time_ms values: %s", browserDesc)
	}
	if !strings.Contains(browserDesc, "session_mode/share_scope") || !strings.Contains(browserDesc, "never invent artifact keys") {
		t.Fatalf("browser description should forbid unsupported mode fields and invented artifacts: %s", browserDesc)
	}

	for _, desc := range []string{python, execDesc, continueDesc} {
		if strings.Contains(desc, "workspace:/relative/path") || strings.Contains(desc, "workspace: links") {
			t.Fatalf("sandbox tool description should not recommend workspace protocol: %s", desc)
		}
		if !strings.Contains(desc, "artifact keys") || !strings.Contains(desc, "invent") {
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
	if !strings.Contains(content, "绝对 `file_path`") {
		t.Fatalf("prompt should mention absolute file_path references: %s", content)
	}
	if !strings.Contains(content, "不要把绝对文件路径改写成 legacy workspace 资源链接") {
		t.Fatalf("prompt should forbid rewriting absolute file paths to legacy workspace links: %s", content)
	}
	if !strings.Contains(content, "`browser:<url>`") {
		t.Fatalf("prompt should preserve browser resource links: %s", content)
	}
	if !strings.Contains(content, "禁止根据 stdout、stderr、本地路径或文件名臆造新的 `artifact:<key>`、`browser:<url>`、legacy workspace 资源链接或绝对文件路径") {
		t.Fatalf("prompt should forbid invented file references: %s", content)
	}
}
