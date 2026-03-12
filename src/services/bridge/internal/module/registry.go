package module

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// modulesFile is the top-level YAML structure of install/modules.yaml.
type modulesFile struct {
	Modules yaml.Node `yaml:"modules"`
}

// yamlModuleDef mirrors a single module entry in the YAML.
type yamlModuleDef struct {
	Category            string             `yaml:"category"`
	Description         string             `yaml:"description"`
	ComposeService      string             `yaml:"compose_service"`
	ComposeProfile      string             `yaml:"compose_profile"`
	DependsOn           []string           `yaml:"depends_on"`
	MutuallyExclusive   []string           `yaml:"mutually_exclusive"`
	Virtual             bool               `yaml:"virtual"`
	Capabilities        ModuleCapabilities `yaml:"capabilities"`
	PlatformConstraints map[string]bool    `yaml:"platform_constraints"`
}

// ModuleDefinition holds parsed YAML data plus computed frontend metadata.
type ModuleDefinition struct {
	ID                  string
	Name                string
	Description         string
	YAMLCategory        string // core | standard | optional
	FrontendCategory    ModuleCategory
	ComposeService      string
	ComposeProfile      string
	DependsOn           []string
	MutuallyExclusive   []string
	Virtual             bool
	Capabilities        ModuleCapabilities
	PlatformConstraints map[string]bool
	Port                *int
}

// ToModuleInfo converts a definition to the API response format with live status.
func (d *ModuleDefinition) ToModuleInfo(status ModuleStatus) ModuleInfo {
	dependsOn := d.DependsOn
	if dependsOn == nil {
		dependsOn = []string{}
	}
	mutuallyExclusive := d.MutuallyExclusive
	if mutuallyExclusive == nil {
		mutuallyExclusive = []string{}
	}

	return ModuleInfo{
		ID:                d.ID,
		Name:              d.Name,
		Description:       d.Description,
		Category:          d.FrontendCategory,
		Status:            status,
		Port:              d.Port,
		Capabilities:      d.Capabilities,
		DependsOn:         dependsOn,
		MutuallyExclusive: mutuallyExclusive,
	}
}

// Registry holds all parsed module definitions.
type Registry struct {
	modules map[string]*ModuleDefinition
	order   []string
}

// LoadRegistry parses install/modules.yaml and returns a populated Registry.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading modules file: %w", err)
	}

	var file modulesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing modules file: %w", err)
	}

	if file.Modules.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node for 'modules', got %d", file.Modules.Kind)
	}

	r := &Registry{
		modules: make(map[string]*ModuleDefinition),
	}

	// MappingNode children alternate: key, value, key, value, ...
	nodes := file.Modules.Content
	for i := 0; i+1 < len(nodes); i += 2 {
		id := nodes[i].Value

		var yDef yamlModuleDef
		if err := nodes[i+1].Decode(&yDef); err != nil {
			return nil, fmt.Errorf("decoding module %q: %w", id, err)
		}

		def := &ModuleDefinition{
			ID:                  id,
			Name:                humanName(id),
			Description:         yDef.Description,
			YAMLCategory:        yDef.Category,
			FrontendCategory:    frontendCategory(id),
			ComposeService:      yDef.ComposeService,
			ComposeProfile:      yDef.ComposeProfile,
			DependsOn:           yDef.DependsOn,
			MutuallyExclusive:   yDef.MutuallyExclusive,
			Virtual:             yDef.Virtual,
			Capabilities:        yDef.Capabilities,
			PlatformConstraints: yDef.PlatformConstraints,
			Port:                knownPort(id),
		}

		r.modules[id] = def
		r.order = append(r.order, id)
	}

	return r, nil
}

// List returns all module definitions in YAML order.
func (r *Registry) List() []ModuleDefinition {
	out := make([]ModuleDefinition, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, *r.modules[id])
	}
	return out
}

// Get returns a module definition by ID.
func (r *Registry) Get(id string) (*ModuleDefinition, bool) {
	def, ok := r.modules[id]
	return def, ok
}

// OptionalModules returns modules shown in the frontend installer UI
// (standard + optional categories, excluding core infrastructure).
func (r *Registry) OptionalModules() []ModuleDefinition {
	out := make([]ModuleDefinition, 0)
	for _, id := range r.order {
		def := r.modules[id]
		if def.YAMLCategory != "core" {
			out = append(out, *def)
		}
	}
	return out
}

// frontendCategory maps a module ID to its frontend display category.
var categoryMap = map[string]ModuleCategory{
	"openviking":         CategoryMemory,
	"sandbox-docker":     CategorySandbox,
	"sandbox-firecracker": CategorySandbox,
	"browser":            CategoryBrowser,
	"searxng":            CategorySearch,
	"firecrawl":          CategorySearch,
	"console":            CategoryConsole,
	"console-lite":       CategoryConsole,
	"postgres":           CategoryInfrastructure,
	"redis":              CategoryInfrastructure,
	"migrate":            CategoryInfrastructure,
	"api":                CategoryInfrastructure,
	"worker":             CategoryInfrastructure,
	"gateway":            CategoryInfrastructure,
	"pgbouncer":          CategoryInfrastructure,
	"seaweedfs":          CategoryInfrastructure,
}

func frontendCategory(id string) ModuleCategory {
	if cat, ok := categoryMap[id]; ok {
		return cat
	}
	return CategoryInfrastructure
}

// knownPort returns the display port for modules that expose one.
var portMap = map[string]int{
	"openviking":         1933,
	"sandbox-docker":     8002,
	"sandbox-firecracker": 8002,
	"searxng":            8888,
	"firecrawl":          3002,
	"console":            5174,
	"console-lite":       5175,
	"api":                8001,
	"gateway":            8000,
}

func knownPort(id string) *int {
	if p, ok := portMap[id]; ok {
		return &p
	}
	return nil
}

// humanName converts a module ID to a display name.
var nameMap = map[string]string{
	"postgres":           "PostgreSQL",
	"redis":              "Redis",
	"migrate":            "Database Migrations",
	"api":                "API Server",
	"worker":             "Worker",
	"gateway":            "Gateway",
	"console-lite":       "Console Lite",
	"console":            "Console",
	"openviking":         "OpenViking",
	"sandbox-docker":     "Sandbox (Docker)",
	"sandbox-firecracker": "Sandbox (Firecracker)",
	"browser":            "Browser Automation",
	"pgbouncer":          "PgBouncer",
	"seaweedfs":          "SeaweedFS",
	"searxng":            "SearXNG",
	"firecrawl":          "Firecrawl",
}

func humanName(id string) string {
	if name, ok := nameMap[id]; ok {
		return name
	}
	return id
}
