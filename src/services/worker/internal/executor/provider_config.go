package executor

import sharedtoolruntime "arkloop/services/shared/toolruntime"

func copyProviderConfigMap(src map[string]sharedtoolruntime.ProviderConfig) map[string]sharedtoolruntime.ProviderConfig {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]sharedtoolruntime.ProviderConfig, len(src))
	for group, cfg := range src {
		out[group] = sharedtoolruntime.ProviderConfig{
			GroupName:    cfg.GroupName,
			ProviderName: cfg.ProviderName,
			BaseURL:      cfg.BaseURL,
			APIKeyValue:  cfg.APIKeyValue,
			ConfigJSON:   copyProviderConfigJSON(cfg.ConfigJSON),
		}
	}
	return out
}

func copyProviderConfigJSON(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
