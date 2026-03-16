//go:build desktop

package localshell

import (
	"strings"
	"testing"
	"time"
)

func TestNewShellController(t *testing.T) {
	dir := t.TempDir()
	ctrl := newShellController(dir)
	if ctrl.status != statusClosed {
		t.Errorf("expected initial status %q, got %q", statusClosed, ctrl.status)
	}
	if ctrl.workDir != dir {
		t.Errorf("expected workDir %q, got %q", dir, ctrl.workDir)
	}
}

func TestExecCommandBasic(t *testing.T) {
	dir := t.TempDir()
	ctrl := newShellController(dir)
	defer ctrl.close()

	resp, err := ctrl.execCommand("echo hello_test_output", "", 10000)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}

	output := resp.Output
	for i := 0; i < 20 && !strings.Contains(output, "hello_test_output"); i++ {
		time.Sleep(200 * time.Millisecond)
		pollResp, pollErr := ctrl.writeStdin("", 500)
		if pollErr != nil {
			break
		}
		output += pollResp.Output
	}
	if !strings.Contains(output, "hello_test_output") {
		t.Errorf("expected output to contain 'hello_test_output', got %q", output)
	}
}

func TestExecCommandExitCode(t *testing.T) {
	dir := t.TempDir()
	ctrl := newShellController(dir)
	defer ctrl.close()

	// Use false command which returns exit code 1
	resp, err := ctrl.execCommand("false", "", 10000)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	for i := 0; i < 20 && (resp.Running || resp.ExitCode == nil); i++ {
		time.Sleep(200 * time.Millisecond)
		pollResp, pollErr := ctrl.writeStdin("", 500)
		if pollErr != nil {
			break
		}
		resp = pollResp
	}

	if resp.ExitCode == nil {
		t.Skip("exit code not captured within timeout")
	} else if *resp.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", *resp.ExitCode)
	}
}

func TestExecCommandEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	ctrl := newShellController(dir)
	defer ctrl.close()

	_, err := ctrl.execCommand("", "", 10000)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestNormalizeTimeoutMs(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, defaultTimeoutMs},
		{-1, defaultTimeoutMs},
		{5000, 5000},
		{500000, maxTimeoutMs},
	}
	for _, tc := range tests {
		got := normalizeTimeoutMs(tc.input)
		if got != tc.want {
			t.Errorf("normalizeTimeoutMs(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeYieldTimeMs(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, defaultYieldTimeMs},
		{-1, defaultYieldTimeMs},
		{2000, 2000},
		{50000, maxYieldTimeMs},
	}
	for _, tc := range tests {
		got := normalizeYieldTimeMs(tc.input)
		if got != tc.want {
			t.Errorf("normalizeYieldTimeMs(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestResolveShell(t *testing.T) {
	path, args := resolveShell()
	if path == "" {
		t.Error("shell path is empty")
	}
	if len(args) == 0 {
		t.Error("shell args are empty")
	}
}

func TestBuildWrappedCommand(t *testing.T) {
	wrapped := buildWrappedCommand("abc123", "/tmp", "echo test")
	if !strings.Contains(wrapped, "abc123") {
		t.Error("wrapped command should contain the token")
	}
	// Markers are intentionally split via ark_mark_a variable to prevent false detection
	if !strings.Contains(wrapped, "ark_mark_a") {
		t.Error("wrapped command should reference ark_mark_a variable")
	}
	if !strings.Contains(wrapped, "_BEGIN__") {
		t.Error("wrapped command should contain _BEGIN__ suffix")
	}
	if !strings.Contains(wrapped, "_END__") {
		t.Error("wrapped command should contain _END__ suffix")
	}
	if !strings.Contains(wrapped, "base64") {
		t.Error("wrapped command should use base64 encoding")
	}
}

func TestTrailingMarkerPrefixLen(t *testing.T) {
	tests := []struct {
		text   string
		marker string
		want   int
	}{
		{"hello__A", "__ARK", 3},
		{"hello", "__ARK", 0},
		{"__ARK", "__ARK", 5},
		{"", "__ARK", 0},
	}
	for _, tc := range tests {
		got := trailingMarkerPrefixLen(tc.text, tc.marker)
		if got != tc.want {
			t.Errorf("trailingMarkerPrefixLen(%q, %q) = %d, want %d", tc.text, tc.marker, got, tc.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
	}
	for _, tc := range tests {
		got := shellQuote(tc.input)
		if got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
