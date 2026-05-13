package pluginmanifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseManifestYAMLAndManifestJSON(t *testing.T) {
	raw := []byte(`
schemaVersion: 1
id: demo.plugin
version: 1.0.0
host_requirement:
  min_version: 0.1.0
platforms: [darwin, linux]
runtime:
  - id: node
    path: bin/node
    detect:
      - path: bin/node
        version_command: [bin/node, --version]
        version_min: 20.0.0
mcp_servers:
  - id: local
    type: stdio
    command: bin/server
    working_dir: .
skills:
  - id: writer
    path: skills/writer
settings:
  - key: mode
    type: string
hooks:
  - event: before_model
    type: command
    command: [bin/hook]
context:
  - id: policy
    path: context/policy.md
`)
	manifest, err := ParseYAML(raw)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.ID != "demo.plugin" || manifest.SchemaVersion != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	encoded, err := manifest.ToManifestJSON()
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode manifest json: %v", err)
	}
	if decoded["schemaVersion"].(float64) != 1 {
		t.Fatalf("unexpected schemaVersion: %v", decoded["schemaVersion"])
	}
}

func TestManifestRejectsUnsupportedFieldsAndUnsafePath(t *testing.T) {
	_, err := ParseYAML([]byte(`
schemaVersion: 1
id: demo
version: 1.0.0
commands: []
`))
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported field error, got %v", err)
	}
	_, err = ParseYAML([]byte(`
schemaVersion: 1
id: demo
version: 1.0.0
skills:
  - id: bad
    path: ../escape
`))
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestParseCUAManifestFragment(t *testing.T) {
	raw := []byte(`
schemaVersion: 1
id: cua
name: Computer Use Agent
version: 0.1.0
publisher: arkloop
description: Browser automation
host_requirement: desktop_local
platforms: [darwin, linux, windows]
runtime:
  detect: [node]
  version_command: [node, --version]
  version_min: 20.0.0
  binary: [bin/cua]
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
    type: select
    label: Profile
    default: default
    options: [default, isolated]
hooks:
  - event: BeforeToolUse
    type: command
    command: "${runtime.cua-driver.path}"
context: docs/cua.md
`)
	manifest, err := ParseYAML(raw)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.HostRequirement != "desktop_local" {
		t.Fatalf("unexpected host requirement: %q", manifest.HostRequirement)
	}
	if len(manifest.Runtime) != 1 || len(manifest.Runtime[0].Detect) != 1 {
		t.Fatalf("unexpected runtime detect: %#v", manifest.Runtime)
	}
	detect := manifest.Runtime[0].Detect[0]
	if detect.Path != "node" || strings.Join(detect.VersionCommand, " ") != "node --version" || detect.VersionMin != "20.0.0" {
		t.Fatalf("unexpected detect config: %#v", detect)
	}
	if got := manifest.MCPServers[0].InstallKey; got != "plugin:cua:browser" {
		t.Fatalf("unexpected install key: %q", got)
	}
	if got := manifest.Skills[0].Version; got != "0.1.0" {
		t.Fatalf("unexpected skill version: %q", got)
	}
	if got := manifest.Runtime[0].Binary[0].Path; got != "bin/cua" {
		t.Fatalf("unexpected runtime binary path: %q", got)
	}
	if got := manifest.Settings[0].Type; got != "select" {
		t.Fatalf("unexpected setting type: %q", got)
	}
	if got := manifest.Hooks[0].Event; got != "BeforeToolUse" {
		t.Fatalf("unexpected hook event: %q", got)
	}
	if got := manifest.Context[0].Path; got != "docs/cua.md" {
		t.Fatalf("unexpected context path: %q", got)
	}
}

func TestParseRuntimeBinaryDistributions(t *testing.T) {
	manifest, err := ParseYAML([]byte(`
schemaVersion: 1
id: arkloop.plugins.cua
version: 0.1.0
runtime:
  id: cua-driver
  detect:
    - "${PLUGIN_DATA}/runtime/cua-driver"
  binary:
    - platform: darwin-arm64
      url: https://example.com/cua-driver.tar.gz
      sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
`))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	binary := manifest.Runtime[0].Binary[0]
	if binary.Platform != "darwin-arm64" || binary.URL == "" || binary.SHA256 == "" {
		t.Fatalf("unexpected binary distribution: %#v", binary)
	}
}

func TestRuntimeDetectAllowsAbsoluteHostPath(t *testing.T) {
	_, err := ParseYAML([]byte(`
schemaVersion: 1
id: arkloop.plugins.cua
version: 0.1.0
runtime:
  id: cua-driver
  detect:
    - "/Applications/CuaDriver.app/Contents/MacOS/cua-driver"
`))
	if err != nil {
		t.Fatalf("parse manifest with absolute detect path: %v", err)
	}
}

func TestManifestRejectsBackslashPathEscapes(t *testing.T) {
	_, err := ParseYAML([]byte(`
schemaVersion: 1
id: demo
version: 1.0.0
skills:
  - id: bad
    path: 'skills\..\secret'
`))
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Fatalf("expected backslash escape rejection, got %v", err)
	}
}

func TestManifestRejectsHookWithoutHandler(t *testing.T) {
	_, err := ParseYAML([]byte(`
schemaVersion: 1
id: demo
version: 1.0.0
hooks:
  - event: BeforeToolUse
`))
	if err == nil || !strings.Contains(err.Error(), "handler") {
		t.Fatalf("expected missing hook handler rejection, got %v", err)
	}
}

func TestHookCommandArgsArePreserved(t *testing.T) {
	manifest, err := ParseYAML([]byte(`
schemaVersion: 1
id: arkloop.plugins.cua
version: 0.1.0
hooks:
  - event: BeforeToolUse
    type: command
    command: "${runtime.cua-driver.path}"
    args: ["hook", "before-tool"]
`))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	hook := manifest.Hooks[0]
	if len(hook.Command) != 1 || hook.Command[0] != "${runtime.cua-driver.path}" {
		t.Fatalf("unexpected command: %#v", hook.Command)
	}
	if len(hook.Args) != 2 || hook.Args[1] != "before-tool" {
		t.Fatalf("unexpected args: %#v", hook.Args)
	}
}

func TestResolveStringRejectsUnknownPlaceholder(t *testing.T) {
	_, err := ResolveString("bin/${unknown}", PlaceholderContext{})
	if err == nil || !strings.Contains(err.Error(), "unknown plugin placeholder") {
		t.Fatalf("expected unknown placeholder error, got %v", err)
	}
}

func TestResolveStringSupportsRuntimeCommandAndHelperAppPath(t *testing.T) {
	got, err := ResolveString("${runtime.cua-driver.command} ${runtime.cua-driver.helperAppPath}", PlaceholderContext{
		RuntimePaths: map[string]string{"cua-driver": "/tmp/CuaDriver.app/Contents/MacOS/cua-driver"},
		RuntimeProperties: map[string]map[string]string{
			"cua-driver": {"helper_app_path": "/tmp/CuaDriver.app"},
		},
	})
	if err != nil {
		t.Fatalf("resolve runtime placeholders: %v", err)
	}
	if got != "/tmp/CuaDriver.app/Contents/MacOS/cua-driver /tmp/CuaDriver.app" {
		t.Fatalf("unexpected resolution: %q", got)
	}
}
