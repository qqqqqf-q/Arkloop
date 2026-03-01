package personas

import "testing"

func TestLoadAutoPersonaExecutorConfig(t *testing.T) {
root, err := BuiltinPersonasRoot()
if err != nil {
t.Fatalf("BuiltinPersonasRoot failed: %v", err)
}
registry, err := LoadRegistry(root)
if err != nil {
t.Fatalf("LoadRegistry failed: %v", err)
}
def, ok := registry.Get("auto")
if !ok {
t.Fatalf("expected auto persona loaded")
}
if def.ExecutorType != "task.classify_route" {
t.Fatalf("expected executor_type 'task.classify_route', got %q", def.ExecutorType)
}

routes, ok := def.ExecutorConfig["routes"]
if !ok {
t.Fatalf("expected executor_config.routes to exist")
}
routesMap, ok := routes.(map[string]any)
if !ok {
t.Fatalf("expected routes to be map[string]any, got %T", routes)
}
for _, key := range []string{"pro", "ultra"} {
entry, ok := routesMap[key]
if !ok {
t.Fatalf("expected route %q to exist", key)
}
entryMap, ok := entry.(map[string]any)
if !ok {
t.Fatalf("expected route %q to be map, got %T", key, entry)
}
if _, ok := entryMap["prompt_override"]; !ok {
t.Fatalf("expected route %q to have prompt_override", key)
}
}

defaultRoute, ok := def.ExecutorConfig["default_route"].(string)
if !ok || defaultRoute != "pro" {
t.Fatalf("expected default_route 'pro', got %q", defaultRoute)
}

classifyPrompt, ok := def.ExecutorConfig["classify_prompt"].(string)
if !ok || classifyPrompt == "" {
t.Fatalf("expected non-empty classify_prompt")
}
}
