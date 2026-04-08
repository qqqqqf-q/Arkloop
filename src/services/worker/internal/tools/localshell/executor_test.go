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
