//go:build desktop

package app

import (
	"os"
	"testing"
)

func TestDesktopJWTSecretValuePersistsGeneratedSecret(t *testing.T) {
	t.Setenv("ARKLOOP_DESKTOP_JWT_SECRET", "")
	dataDir := t.TempDir()

	first, err := desktopJWTSecretValue(dataDir)
	if err != nil {
		t.Fatalf("desktopJWTSecretValue first: %v", err)
	}
	second, err := desktopJWTSecretValue(dataDir)
	if err != nil {
		t.Fatalf("desktopJWTSecretValue second: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("secret was not persisted: first=%q second=%q", first, second)
	}
	info, err := os.Stat(dataDir + "/jwt.secret")
	if err != nil {
		t.Fatalf("stat jwt.secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("jwt.secret mode = %o, want 600", info.Mode().Perm())
	}
}
