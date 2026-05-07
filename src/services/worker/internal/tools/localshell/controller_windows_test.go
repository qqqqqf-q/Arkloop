//go:build desktop && windows

package localshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestProcessControllerBufferedCommandReturnsStdoutOnWindows(t *testing.T) {
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		t.Skip("SystemRoot is not set")
	}

	t.Setenv("PATH", strings.Join([]string{
		filepath.Join(systemRoot, "System32"),
		systemRoot,
	}, string(os.PathListSeparator)))

	controller := NewProcessController()
	resp, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "echo hello",
		Mode:      ModeBuffered,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resp.Status != StatusExited {
		t.Fatalf("expected exited status, got %#v", resp)
	}
	if strings.TrimSpace(resp.Stdout) != "hello" {
		t.Fatalf("expected stdout hello, got %#v", resp.Stdout)
	}
	if strings.TrimSpace(resp.Stderr) != "" {
		t.Fatalf("expected empty stderr, got %#v", resp.Stderr)
	}
}

func TestProcessOutputEncodingForCodePage936DecodesChinese(t *testing.T) {
	enc := processOutputEncodingForCodePage(936)
	if enc == nil {
		t.Fatal("expected decoder for code page 936")
	}
	raw, _, err := transform.String(simplifiedchinese.GB18030.NewEncoder(), "\"Hello,喵!\"")
	if err != nil {
		t.Fatalf("encode text: %v", err)
	}
	decoded, _, err := transform.String(enc.NewDecoder(), raw)
	if err != nil {
		t.Fatalf("decode text: %v", err)
	}
	if decoded != "\"Hello,喵!\"" {
		t.Fatalf("expected decoded text preserved, got %q", decoded)
	}
}

func TestWindowsProcessOutputEncodingHonorsUtf8ConsoleCodePage(t *testing.T) {
	consoleCodePage := uint32(65001)
	if processOutputEncodingForCodePage(consoleCodePage) != nil {
		t.Fatal("expected UTF-8 console code page to bypass decoder")
	}
	acp := uint32(1252)
	if processOutputEncodingForCodePage(acp) == nil {
		t.Fatal("expected ACP decoder to exist for fallback code page")
	}
}
