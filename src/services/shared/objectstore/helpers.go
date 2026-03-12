package objectstore

import "strings"

func normalizeS3Config(cfg S3Config) S3Config {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
	cfg.SecretKey = strings.TrimSpace(cfg.SecretKey)
	cfg.Region = strings.TrimSpace(cfg.Region)
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return cfg
}

func normalizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		cleaned[normalizedKey] = strings.TrimSpace(value)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}
