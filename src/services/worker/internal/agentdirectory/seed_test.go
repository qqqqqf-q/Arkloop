package agentdirectory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedWorkspace(t *testing.T) {
	dir := t.TempDir()

	count, err := SeedWorkspace(dir)
	if err != nil {
		t.Fatalf("SeedWorkspace: %v", err)
	}
	if count != len(templateNames) {
		t.Errorf("seeded %d files, want %d", count, len(templateNames))
	}

	for _, name := range templateNames {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("template %s not found after seeding: %v", name, err)
		}
	}

	// Seeding again should not overwrite.
	count2, err := SeedWorkspace(dir)
	if err != nil {
		t.Fatalf("second SeedWorkspace: %v", err)
	}
	if count2 != 0 {
		t.Errorf("second seeding wrote %d files, want 0", count2)
	}
}

func TestSeedWorkspacePartial(t *testing.T) {
	dir := t.TempDir()

	// Pre-create one file.
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := SeedWorkspace(dir)
	if err != nil {
		t.Fatalf("SeedWorkspace: %v", err)
	}
	if count != len(templateNames)-1 {
		t.Errorf("seeded %d files, want %d", count, len(templateNames)-1)
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Error("SeedWorkspace overwrote pre-existing file")
	}
}

func TestIsBootstrapPending(t *testing.T) {
	dir := t.TempDir()

	if IsBootstrapPending(dir) {
		t.Error("bootstrap pending on empty dir")
	}

	if _, err := SeedWorkspace(dir); err != nil {
		t.Fatal(err)
	}
	if !IsBootstrapPending(dir) {
		t.Error("bootstrap not pending after seeding")
	}

	os.Remove(filepath.Join(dir, "BOOTSTRAP.md"))
	if IsBootstrapPending(dir) {
		t.Error("bootstrap pending after removal")
	}
}

func TestEmbeddedTemplatesExist(t *testing.T) {
	for _, name := range templateNames {
		data, err := templateFS.ReadFile(filepath.Join("templates", name))
		if err != nil {
			t.Errorf("embedded template %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Errorf("embedded template %s is empty", name)
		}
	}
}
