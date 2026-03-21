package personas

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"arkloop/services/api/internal/data"
	"gopkg.in/yaml.v3"
)

func BuiltinPersonasRoot() (string, error) {
	if envRoot := os.Getenv("ARKLOOP_PERSONAS_ROOT"); envRoot != "" {
		return envRoot, nil
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate personas root directory")
	}
	dir := filepath.Dir(filename)
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Join(dir, "personas"), nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return "", fmt.Errorf("src directory not found, cannot locate personas root directory")
}

type RepoPersona struct {
	ID                  string         `yaml:"id"`
	Version             string         `yaml:"version"`
	Title               string         `yaml:"title"`
	Description         string         `yaml:"description"`
	UserSelectable      bool           `yaml:"user_selectable"`
	SelectorName        string         `yaml:"selector_name"`
	SelectorOrder       *int           `yaml:"selector_order"`
	ToolAllowlist       []string       `yaml:"tool_allowlist"`
	ToolDenylist        []string       `yaml:"tool_denylist"`
	CoreTools           []string       `yaml:"core_tools"`
	Budgets             map[string]any `yaml:"budgets"`
	TitleSummarize      map[string]any `yaml:"title_summarize"`
	PreferredCredential string         `yaml:"preferred_credential"`
	Model               string         `yaml:"model"`
	ReasoningMode       string         `yaml:"reasoning_mode"`
	StreamThinking      *bool          `yaml:"stream_thinking,omitempty"`
	PromptCacheControl  string         `yaml:"prompt_cache_control"`
	ExecutorType        string         `yaml:"executor_type"`
	ExecutorConfig      map[string]any `yaml:"executor_config"`
	Roles               map[string]any `yaml:"roles"`
	SoulFile            string         `yaml:"soul_file"`
	IsSystem            bool           `yaml:"is_system"`
	IsBuiltin           bool           `yaml:"is_builtin"`
	DirName             string         `yaml:"-"`
	SoulMD              string         `yaml:"-"`
	PromptMD            string         `yaml:"-"`
}

func LoadFromDir(root string) ([]RepoPersona, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []RepoPersona
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		yamlPath := filepath.Join(dir, "persona.yaml")
		promptPath := filepath.Join(dir, "prompt.md")

		yamlData, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}

		var p RepoPersona
		if err := yaml.Unmarshal(yamlData, &p); err != nil {
			continue
		}
		var rawObj map[string]any
		if err := yaml.Unmarshal(yamlData, &rawObj); err != nil {
			continue
		}
		if p.ID == "" {
			continue
		}
		p.DirName = entry.Name()
		if p.Version == "" {
			p.Version = "1"
		}
		roles, err := data.NormalizePersonaRolesValue(rawObj["roles"])
		if err != nil {
			return nil, fmt.Errorf("persona %s roles: %w", p.ID, err)
		}
		if len(roles) > 0 {
			p.Roles = roles
		}
		if promptData, err := os.ReadFile(promptPath); err == nil {
			p.PromptMD = strings.TrimSpace(string(promptData))
		}

		if err := inlineLuaScriptConfig(&p, dir); err != nil {
			return nil, fmt.Errorf("persona %s executor_config: %w", p.ID, err)
		}

		soulFile, soulExplicit, err := parseSoulFile(rawObj)
		if err != nil {
			return nil, fmt.Errorf("persona %s: %w", p.ID, err)
		}
		soulPath, err := resolvePersonaLocalPath(dir, soulFile)
		if err != nil {
			if soulExplicit {
				return nil, fmt.Errorf("persona %s soul_file: %w", p.ID, err)
			}
		} else if soulData, err := os.ReadFile(soulPath); err == nil {
			p.SoulMD = strings.TrimSpace(string(soulData))
		} else if soulExplicit || !os.IsNotExist(err) {
			return nil, fmt.Errorf("persona %s soul_file: %w", p.ID, err)
		}
		if soulExplicit && p.SoulMD == "" {
			return nil, fmt.Errorf("persona %s soul_file: file must not be empty", p.ID)
		}

		result = append(result, p)
	}
	return result, nil
}

func inlineLuaScriptConfig(persona *RepoPersona, personaDir string) error {
	if persona == nil || strings.TrimSpace(persona.ExecutorType) != "agent.lua" {
		return nil
	}
	if persona.ExecutorConfig == nil {
		persona.ExecutorConfig = map[string]any{}
	}
	if script, ok := persona.ExecutorConfig["script"].(string); ok && strings.TrimSpace(script) != "" {
		delete(persona.ExecutorConfig, "script_file")
		return nil
	}
	rawScriptFile, ok := persona.ExecutorConfig["script_file"].(string)
	if !ok || strings.TrimSpace(rawScriptFile) == "" {
		return nil
	}
	scriptPath, err := resolvePersonaLocalPath(personaDir, rawScriptFile)
	if err != nil {
		return err
	}
	rawScript, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	script := strings.TrimSpace(string(rawScript))
	if script == "" {
		return fmt.Errorf("script_file must not be empty")
	}
	persona.ExecutorConfig["script"] = script
	delete(persona.ExecutorConfig, "script_file")
	return nil
}

func parseSoulFile(obj map[string]any) (string, bool, error) {
	const defaultSoulFile = "soul.md"
	rawSoulFile, ok := obj["soul_file"]
	if !ok {
		return defaultSoulFile, false, nil
	}
	soulFile, ok := rawSoulFile.(string)
	if !ok {
		return "", true, fmt.Errorf("soul_file must be a string")
	}
	soulFile = strings.TrimSpace(soulFile)
	if soulFile == "" {
		return "", true, fmt.Errorf("soul_file must not be empty")
	}
	return soulFile, true, nil
}

func resolvePersonaLocalPath(personaDir string, pathValue string) (string, error) {
	if filepath.IsAbs(pathValue) {
		return "", fmt.Errorf("must be a relative path")
	}
	cleaned := filepath.Clean(pathValue)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes persona directory")
	}
	return filepath.Join(personaDir, cleaned), nil
}
