package plugincontrib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/api/internal/data"
	sharedpluginmanifest "arkloop/services/shared/pluginmanifest"
	"github.com/google/uuid"
)

func TestCUAManifestDerivesMCPInstallKeyAndLaunchSpec(t *testing.T) {
	raw := []byte(`
schemaVersion: 1
id: cua
name: Computer Use Agent
version: 0.1.0
publisher: arkloop
host_requirement: desktop_local
runtime:
  detect: [node]
  version_command: [node, --version]
  version_min: 20.0.0
mcp_servers:
  - server_id: browser
    transport: stdio
    command: npx
    args: ["@playwright/mcp", "--profile", "${settings.profile}"]
    env:
      CUA_PROFILE: "${settings.profile}"
skills:
  - skill_key: cua.browser
    bundle: skills/cua
settings:
  - key: profile
    type: string
    default: default
`)
	manifest, normalized, err := decodeManifest(raw)
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if !json.Valid(normalized) {
		t.Fatalf("normalized manifest is not json")
	}
	if got := manifest.MCPServers[0].InstallKey; got != "plugin:cua:browser" {
		t.Fatalf("unexpected manifest install key: %q", got)
	}
	install, err := buildMCPInstall(uuid.New(), "profile_ref", manifest, manifest.MCPServers[0], map[string]any{"profile": "default"}, nil, false)
	if err != nil {
		t.Fatalf("build mcp install: %v", err)
	}
	if got := install.InstallKey; got != "plugin:cua:browser" {
		t.Fatalf("unexpected install key: %q", got)
	}
	if install.HostRequirement != "desktop_local" {
		t.Fatalf("unexpected host requirement: %q", install.HostRequirement)
	}
	var spec map[string]any
	if err := json.Unmarshal(install.LaunchSpecJSON, &spec); err != nil {
		t.Fatalf("decode launch spec: %v", err)
	}
	args, _ := spec["args"].([]any)
	if len(args) != 3 || args[2] != "default" {
		t.Fatalf("unexpected launch spec args: %#v", spec["args"])
	}
	env, _ := spec["env"].(map[string]any)
	if env["CUA_PROFILE"] != "default" {
		t.Fatalf("unexpected launch spec env: %#v", spec["env"])
	}
}

func TestRenderLaunchSpecResolvesRuntimePath(t *testing.T) {
	spec := map[string]any{
		"command": "${runtime.cua-driver.path}",
		"args":    []any{"mcp", "--log", "${settings.log_level}"},
	}
	payload, err := renderLaunchSpec(spec, map[string]any{"log_level": "debug"}, map[string]any{
		"cua-driver.path": "/tmp/cua-driver",
		"plugin_data":     "/tmp/plugin",
	}, true)
	if err != nil {
		t.Fatalf("render launch spec: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode launch spec: %v", err)
	}
	if decoded["command"] != "/tmp/cua-driver" {
		t.Fatalf("unexpected command: %#v", decoded)
	}
	args := decoded["args"].([]any)
	if args[1] != "--log" || args[2] != "debug" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestRenderLaunchSpecResolvesRuntimeCommandHelperAndPlatform(t *testing.T) {
	spec := map[string]any{
		"command": "${runtime.cua-driver.command}",
		"env": map[string]any{
			"ARKLOOP_CUA_HELPER_APP_PATH": "${runtime.cua-driver.helper_app_path}",
			"ARKLOOP_CUA_PLATFORM":        "${platform}-${arch}",
		},
	}
	payload, err := renderLaunchSpec(spec, nil, map[string]any{
		"cua-driver.path":            "/tmp/CuaDriver.app/Contents/MacOS/cua-driver",
		"cua-driver.helper_app_path": "/tmp/CuaDriver.app",
	}, true)
	if err != nil {
		t.Fatalf("render launch spec: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode launch spec: %v", err)
	}
	if decoded["command"] != "/tmp/CuaDriver.app/Contents/MacOS/cua-driver" {
		t.Fatalf("unexpected command: %#v", decoded)
	}
	env := decoded["env"].(map[string]any)
	if env["ARKLOOP_CUA_HELPER_APP_PATH"] != "/tmp/CuaDriver.app" {
		t.Fatalf("unexpected helper env: %#v", env)
	}
	if !strings.Contains(env["ARKLOOP_CUA_PLATFORM"].(string), "-") {
		t.Fatalf("unexpected platform env: %#v", env)
	}
}

func TestRenderLaunchSpecRejectsMissingRuntimePathWhenStrict(t *testing.T) {
	_, err := renderLaunchSpec(map[string]any{
		"command": "${runtime.cua-driver.path}",
	}, nil, nil, true)
	if err == nil {
		t.Fatalf("expected missing runtime placeholder error")
	}
}

func TestValidatePluginHooksRejectsMissingRuntimePathWhenEnabled(t *testing.T) {
	manifest, _, err := decodeManifest([]byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
hooks:
  - event: BeforeToolUse
    type: command
    command: ["${runtime.driver.path}"]
`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := validatePluginHooks(manifest, nil, nil, true); err == nil {
		t.Fatalf("expected missing runtime placeholder error")
	}
}

func TestValidatePluginHooksAcceptsPluginDataCommand(t *testing.T) {
	manifest, _, err := decodeManifest([]byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
hooks:
  - event: BeforeToolUse
    type: command
    command: ["${PLUGIN_DATA}/hook"]
`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := validatePluginHooks(manifest, nil, map[string]any{"plugin_data": "/tmp/plugin"}, true); err != nil {
		t.Fatalf("validate hooks: %v", err)
	}
}

func TestParseCUAPermission(t *testing.T) {
	output := "Accessibility: granted.\nScreen Recording: not granted."
	if granted, ok := parseCUAPermission(output, "accessibility"); !ok || !granted {
		t.Fatalf("expected accessibility granted, got granted=%v ok=%v", granted, ok)
	}
	if granted, ok := parseCUAPermission(output, "screen recording"); !ok || granted {
		t.Fatalf("expected screen recording denied, got granted=%v ok=%v", granted, ok)
	}
	if _, ok := parseCUAPermission(output, "camera"); ok {
		t.Fatalf("expected camera permission to be absent")
	}

	diagnose := "accessibility      (AXIsProcessTrusted):  true\nscreen recording   (SCShareableContent):  true"
	if granted, ok := parseCUAPermission(diagnose, "screen recording"); !ok || !granted {
		t.Fatalf("expected diagnose screen recording granted, got granted=%v ok=%v", granted, ok)
	}
}

func TestPreserveRuntimeCheckStatusOnlyCopiesPermissionKeys(t *testing.T) {
	current := &data.PluginRuntimeState{StatusJSON: json.RawMessage(`{
		"plugin_data": "/old",
		"cua-driver.status": "installed",
		"cua-driver.permissions.accessibility": true,
		"cua-driver.permissions.screen_recording": false
	}`)}
	next := map[string]any{
		"plugin_data":       "/new",
		"cua-driver.status": "not_installed",
	}
	if !preserveRuntimeCheckStatus(next, current) {
		t.Fatalf("expected permission state to be copied")
	}
	if next["plugin_data"] != "/new" || next["cua-driver.status"] != "not_installed" {
		t.Fatalf("unexpected non-permission overwrite: %#v", next)
	}
	if next["cua-driver.permissions.accessibility"] != true || next["cua-driver.permissions.screen_recording"] != false {
		t.Fatalf("unexpected permission state: %#v", next)
	}
}

func TestNormalizeSettingsAppliesDefaultsAndValidatesOptions(t *testing.T) {
	manifest, _, err := decodeManifest([]byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
settings:
  - key: log_level
    type: select
    options: [info, debug]
    default: info
  - key: enabled
    type: boolean
    default: true
`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	payload, settings, err := normalizeSettings(nil, manifest)
	if err != nil {
		t.Fatalf("normalize defaults: %v", err)
	}
	if settings["log_level"] != "info" || settings["enabled"] != true || !json.Valid(payload) {
		t.Fatalf("unexpected settings: %#v", settings)
	}
	_, _, err = normalizeSettings(map[string]any{"log_level": "trace"}, manifest)
	if err == nil {
		t.Fatalf("expected select option validation error")
	}
}

func TestNormalizeSettingsValidatesSettingsSchemaDefaults(t *testing.T) {
	manifest, _, err := decodeManifest([]byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
settings_schema:
  log_level:
    type: select
    options: [info, debug]
    default: trace
`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	_, _, err = normalizeSettings(nil, manifest)
	if err == nil {
		t.Fatalf("expected settings_schema default validation error")
	}
}

func TestValidatePluginHostRejectsServerDesktopRequirementOutsideDesktop(t *testing.T) {
	if pluginHostModeDesktop {
		t.Skip("desktop build allows desktop plugin host requirements")
	}
	manifest, _, err := decodeManifest([]byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
mcp_servers:
  - server_id: local
    transport: stdio
    command: bin/server
    host_requirement: desktop_local
`))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := validatePluginHost(manifest); err == nil {
		t.Fatalf("expected desktop_local server host rejection")
	}
}

func TestSafePluginPathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../secret", "skills/../../secret", `skills\..\secret`, "/tmp/secret", `C:\tmp\secret`} {
		if _, err := safePluginPath(root, rel); err == nil || !strings.Contains(err.Error(), "plugin path") {
			t.Fatalf("expected unsafe plugin path rejection for %q, got %v", rel, err)
		}
	}
	path, err := safePluginPath(root, "skills/cua/SKILL.md")
	if err != nil {
		t.Fatalf("safe plugin path: %v", err)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("expected path under root, got %q", path)
	}
}

func TestRegistryBundlePathRejectsAbsoluteEntries(t *testing.T) {
	if _, err := registryBundlePath("/manifest.yaml"); err == nil {
		t.Fatalf("expected absolute bundle entry rejection")
	}
}

func TestPluginFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(root, "context.md")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readPluginFile(root, link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestBuildPluginSkillBundleRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("create skills dir: %v", err)
	}
	outside := t.TempDir()
	outsideSkill := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(outsideSkill, []byte("# Secret\n"), 0o600); err != nil {
		t.Fatalf("write outside skill: %v", err)
	}
	if err := os.Symlink(outsideSkill, filepath.Join(skillsDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := buildPluginSkillBundle(
		Manifest{ID: "demo.plugin", Version: "1.0.0"},
		sharedpluginmanifest.SkillConfig{SkillKey: "demo.skill", Version: "1.0.0"},
		root,
		skillsDir,
	)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected skill symlink rejection, got %v", err)
	}
}
