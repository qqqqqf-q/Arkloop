package config

import "context"

type SettingValue struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

type SettingLayers struct {
	Env        *string `json:"env"`
	ProjectDB  *string `json:"project_db"`
	PlatformDB *string `json:"platform_db"`
	Default    string  `json:"default"`
}

type SettingInspection struct {
	Key         string        `json:"key"`
	Type        string        `json:"type"`
	Scope       string        `json:"scope"`
	Description string        `json:"description"`
	EnvKeys     []string      `json:"env_keys"`
	Effective   SettingValue  `json:"effective"`
	Layers      SettingLayers `json:"layers"`
}

func Inspect(ctx context.Context, registry *Registry, store Store, key string, scope Scope) (SettingInspection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if registry == nil {
		registry = DefaultRegistry()
	}

	entry, ok := registry.Get(key)
	if !ok {
		return SettingInspection{}, errConfigKeyNotRegistered(key)
	}

	inspection := SettingInspection{
		Key:         entry.Key,
		Type:        entry.Type,
		Scope:       entry.Scope,
		Description: entry.Description,
		EnvKeys:     envKeysForEntry(entry),
		Layers: SettingLayers{
			Default: entry.Default,
		},
		Effective: SettingValue{
			Value:  entry.Default,
			Source: "default",
		},
	}

	if value, ok := resolveFromEnv(entry); ok {
		inspection.Layers.Env = stringPtr(value)
		inspection.Effective = SettingValue{Value: value, Source: "env"}
	}

	if scope.ProjectID != nil && (entry.Scope == ScopeProject || entry.Scope == ScopeBoth) && store != nil {
		value, found, err := store.GetProjectSetting(ctx, *scope.ProjectID, entry.Key)
		if err != nil {
			return SettingInspection{}, err
		}
		if found {
			inspection.Layers.ProjectDB = stringPtr(value)
			if inspection.Effective.Source == "default" {
				inspection.Effective = SettingValue{Value: value, Source: "project_db"}
			}
		}
	}

	if (entry.Scope == ScopePlatform || entry.Scope == ScopeBoth) && store != nil {
		value, found, err := store.GetPlatformSetting(ctx, entry.Key)
		if err != nil {
			return SettingInspection{}, err
		}
		if found {
			inspection.Layers.PlatformDB = stringPtr(value)
			if inspection.Effective.Source == "default" {
				inspection.Effective = SettingValue{Value: value, Source: "platform_db"}
			}
		}
	}

	if inspection.Effective.Source == "default" && inspection.Layers.ProjectDB != nil {
		inspection.Effective = SettingValue{Value: *inspection.Layers.ProjectDB, Source: "project_db"}
	}
	if inspection.Effective.Source == "default" && inspection.Layers.PlatformDB != nil {
		inspection.Effective = SettingValue{Value: *inspection.Layers.PlatformDB, Source: "platform_db"}
	}

	return inspection, nil
}

func envKeysForEntry(entry Entry) []string {
	keys := entry.EnvKeys
	if len(keys) == 0 {
		keys = []string{deriveEnvKey(entry.Key)}
	}
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

func stringPtr(value string) *string {
	copy := value
	return &copy
}

func errConfigKeyNotRegistered(key string) error {
	return &configKeyError{key: key}
}

type configKeyError struct {
	key string
}

func (e *configKeyError) Error() string {
	return "config key not registered: " + e.key
}
