package toolmeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellToolDescriptionsUseLocalMachineSemantics(t *testing.T) {
	if _, ok := Lookup("python_execute"); ok {
		t.Fatal("python_execute should no longer be registered")
	}
	if _, ok := Lookup("browser"); ok {
		t.Fatal("browser should no longer be registered")
	}

	execDesc := Must("exec_command").LLMDescription
	if !strings.Contains(execDesc, "user's local machine") {
		t.Fatalf("exec_command description should state local machine execution: %s", execDesc)
	}
	if !strings.Contains(execDesc, "exact absolute file_path") {
		t.Fatalf("exec_command description should prefer absolute file paths: %s", execDesc)
	}
	for _, stale := range []string{"/tmp/output/", "/workspace/", "sandbox"} {
		if strings.Contains(execDesc, stale) {
			t.Fatalf("exec_command description should not reference %q: %s", stale, execDesc)
		}
	}

	continueDesc := Must("continue_process").LLMDescription
	if !strings.Contains(continueDesc, "process_ref") || !strings.Contains(continueDesc, "exact absolute file_path") {
		t.Fatalf("continue_process description should mention process_ref and absolute file paths: %s", continueDesc)
	}

	for _, name := range []string{"exec_command", "continue_process", "terminate_process", "resize_process"} {
		if Must(name).Group != GroupShell {
			t.Fatalf("%s should be in shell group", name)
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
