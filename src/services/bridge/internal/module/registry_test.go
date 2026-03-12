package module

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// findRepoRoot walks up from cwd until it finds a directory containing ".git".
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (.git)")
		}
		dir = parent
	}
}

func modulesYAMLPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "install", "modules.yaml")
}

func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry(modulesYAMLPath(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	list := reg.List()
	if len(list) < 10 {
		t.Fatalf("expected >10 modules, got %d", len(list))
	}

	knownModules := []string{"postgres", "redis", "openviking", "sandbox-docker", "api", "worker", "gateway"}
	for _, id := range knownModules {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("expected module %q to exist in registry", id)
		}
	}
}

func TestRegistryGet(t *testing.T) {
	reg, err := LoadRegistry(modulesYAMLPath(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	def, ok := reg.Get("openviking")
	if !ok {
		t.Fatal("expected openviking to exist")
	}

	if def.Name != "OpenViking" {
		t.Errorf("Name = %q, want %q", def.Name, "OpenViking")
	}
	if def.FrontendCategory != CategoryMemory {
		t.Errorf("FrontendCategory = %q, want %q", def.FrontendCategory, CategoryMemory)
	}
	if !def.Capabilities.BootstrapSupported {
		t.Error("expected BootstrapSupported to be true")
	}
	if def.Port == nil || *def.Port != 1933 {
		t.Errorf("Port = %v, want 1933", def.Port)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg, err := LoadRegistry(modulesYAMLPath(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected Get(nonexistent) to return false")
	}
}

func TestOptionalModules(t *testing.T) {
	reg, err := LoadRegistry(modulesYAMLPath(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	optional := reg.OptionalModules()
	coreIDs := map[string]struct{}{
		"postgres": {}, "redis": {}, "migrate": {},
	}

	for _, m := range optional {
		if _, isCore := coreIDs[m.ID]; isCore {
			t.Errorf("OptionalModules should exclude core module %q", m.ID)
		}
	}

	if len(optional) == 0 {
		t.Fatal("expected at least one optional module")
	}
}

func TestToModuleInfo(t *testing.T) {
	port := 9999
	def := &ModuleDefinition{
		ID:               "test-mod",
		Name:             "Test Module",
		Description:      "A test module",
		FrontendCategory: CategorySearch,
		Port:             &port,
		Capabilities: ModuleCapabilities{
			Installable:    true,
			BootstrapSupported: true,
		},
		DependsOn:         nil,
		MutuallyExclusive: nil,
	}

	info := def.ToModuleInfo("running")

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Verify key JSON field names
	for _, field := range []string{"id", "name", "description", "category", "status", "capabilities", "depends_on", "mutually_exclusive"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected JSON field %q to be present", field)
		}
	}

	// depends_on and mutually_exclusive must never be null
	if string(raw["depends_on"]) == "null" {
		t.Error("depends_on must not be null")
	}
	if string(raw["mutually_exclusive"]) == "null" {
		t.Error("mutually_exclusive must not be null")
	}
}

func TestFrontendCategoryMapping(t *testing.T) {
	reg, err := LoadRegistry(modulesYAMLPath(t))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	tests := []struct {
		id       string
		expected ModuleCategory
	}{
		{"openviking", CategoryMemory},
		{"sandbox-docker", CategorySandbox},
		{"searxng", CategorySearch},
		{"browser", CategoryBrowser},
		{"console", CategoryConsole},
		{"postgres", CategoryInfrastructure},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			def, ok := reg.Get(tc.id)
			if !ok {
				t.Fatalf("module %q not found", tc.id)
			}
			if def.FrontendCategory != tc.expected {
				t.Errorf("FrontendCategory = %q, want %q", def.FrontendCategory, tc.expected)
			}
		})
	}
}
