//go:build desktop

package localshell

import "testing"

func TestSanitizeLocalEnvPatchesUnsetsHostSpecificVariables(t *testing.T) {
	t.Setenv("ARKLOOP_EXEC_SANITIZE_TEST", "secret")
	t.Setenv("HOME", "/tmp/home")

	patches := sanitizeLocalEnvPatches(nil)
	if patches == nil {
		t.Fatal("expected patches to remove host-only variables")
	}
	value, ok := patches["ARKLOOP_EXEC_SANITIZE_TEST"]
	if !ok || value != nil {
		t.Fatalf("expected host variable unset patch, got %#v", patches["ARKLOOP_EXEC_SANITIZE_TEST"])
	}
	if _, ok := patches["HOME"]; ok {
		t.Fatalf("expected HOME to remain allowed, got %#v", patches["HOME"])
	}
}

func TestSanitizeLocalEnvPatchesKeepsWindowsRuntimeVariables(t *testing.T) {
	t.Setenv("SystemRoot", `C:\Windows`)
	t.Setenv("WINDIR", `C:\Windows`)
	t.Setenv("ComSpec", `C:\Windows\System32\cmd.exe`)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("USERPROFILE", `C:\Users\arkloop`)

	patches := sanitizeLocalEnvPatches(nil)
	for _, key := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT", "USERPROFILE"} {
		if value, ok := patches[key]; ok {
			t.Fatalf("expected %s to remain allowed, got %#v", key, value)
		}
	}
}

func TestSanitizeOutputPreservesWindowsCRLF(t *testing.T) {
	got := sanitizeOutput("hello\r\n")
	if got != "hello\n" {
		t.Fatalf("expected CRLF output to be preserved, got %q", got)
	}
}

func TestSanitizeOutputKeepsLastCarriageReturnSegment(t *testing.T) {
	got := sanitizeOutput("step 1\rstep 2\rfinal")
	if got != "final" {
		t.Fatalf("expected carriage-return overwrite to keep final segment, got %q", got)
	}
}
