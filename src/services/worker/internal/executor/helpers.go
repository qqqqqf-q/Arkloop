package executor

import (
	"fmt"
	"strings"

	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

func requiredString(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return cleaned, nil
}

func requiredUUID(values map[string]any, key string) (uuid.UUID, error) {
	text, err := requiredString(values, key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s is not a valid UUID", key)
	}
	return id, nil
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

// agentIDFromSkill 从 RunContext 的 SkillDefinition 中提取 agent_id。
func agentIDFromSkill(rc *pipeline.RunContext) string {
	if rc.SkillDefinition != nil && strings.TrimSpace(rc.SkillDefinition.ID) != "" {
		return rc.SkillDefinition.ID
	}
	return "default"
}
