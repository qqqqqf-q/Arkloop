package config

import (
	"context"
	"fmt"
	"strings"
)

// ProfileMapping holds the resolved provider and model for a profile name.
type ProfileMapping struct {
	Provider string // e.g., "anthropic", "openai"
	Model    string // e.g., "claude-sonnet-4-5"
}

// ResolveProfile resolves a profile name (e.g., "fast", "balanced", "strong")
// to a provider^model pair using the config resolver.
// The config key format is: spawn.profile.{name}
// The value format is: provider^model (e.g., "anthropic^claude-sonnet-4-5")
func ResolveProfile(ctx context.Context, resolver Resolver, profileName string) (*ProfileMapping, error) {
	if profileName == "" {
		return nil, fmt.Errorf("profile name must not be empty")
	}

	key := "spawn.profile." + strings.TrimSpace(strings.ToLower(profileName))

	val, err := resolver.Resolve(ctx, key, Scope{})
	if err != nil {
		return nil, fmt.Errorf("resolve profile %q: %w", profileName, err)
	}

	return ParseProfileValue(val)
}

// ParseProfileValue parses a "provider^model" string into a ProfileMapping.
func ParseProfileValue(value string) (*ProfileMapping, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("profile value is empty")
	}

	parts := strings.SplitN(value, "^", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("invalid profile value %q: expected format provider^model", value)
	}

	return &ProfileMapping{
		Provider: strings.TrimSpace(parts[0]),
		Model:    strings.TrimSpace(parts[1]),
	}, nil
}
