package skills

import (
	"fmt"
	"strings"
)

const (
	ErrorClassSkillNotFound        = "skill.not_found"
	ErrorClassSkillVersionMismatch = "skill.version_mismatch"
	ErrorClassSkillInvalidID       = "skill.invalid_id"
)

type Resolution struct {
	Definition *Definition
	Error      *ResolutionError
}

type ResolutionError struct {
	ErrorClass string
	Message    string
	Details    map[string]any
}

func ResolveSkill(inputJSON map[string]any, registry *Registry) Resolution {
	if registry == nil {
		return Resolution{}
	}

	raw, exists := inputJSON["skill_id"]
	if !exists || raw == nil {
		return Resolution{}
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassSkillInvalidID,
				Message:    "skill_id 非法",
			},
		}
	}

	skillID, requestedVersion, err := parseSkillRef(strings.TrimSpace(text))
	if err != nil {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassSkillInvalidID,
				Message:    "skill_id 非法",
			},
		}
	}

	def, ok := registry.Get(skillID)
	if !ok {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassSkillNotFound,
				Message:    "skill 不存在",
				Details:    map[string]any{"skill_id": skillID},
			},
		}
	}

	if requestedVersion != "" && requestedVersion != def.Version {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassSkillVersionMismatch,
				Message:    "skill 版本不匹配",
				Details: map[string]any{
					"skill_id":          skillID,
					"requested_version": requestedVersion,
					"available_version": def.Version,
				},
			},
		}
	}

	return Resolution{
		Definition: &def,
	}
}

func parseSkillRef(value string) (string, string, error) {
	skillID, version, hasSep := strings.Cut(value, "@")
	if !hasSep {
		return value, "", nil
	}
	skillID = strings.TrimSpace(skillID)
	version = strings.TrimSpace(version)
	if skillID == "" {
		return "", "", fmt.Errorf("skill_id 为空")
	}
	if version == "" {
		return "", "", fmt.Errorf("skill_id@version 形式缺少 version")
	}
	return skillID, version, nil
}

