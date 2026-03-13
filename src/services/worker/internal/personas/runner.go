package personas

import (
	"fmt"
	"strings"
)

const (
	ErrorClassPersonaNotFound        = "persona.not_found"
	ErrorClassPersonaVersionMismatch = "persona.version_mismatch"
	ErrorClassPersonaInvalidID       = "persona.invalid_id"
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

func ResolvePersona(inputJSON map[string]any, registry *Registry) Resolution {
	if registry == nil {
		return Resolution{}
	}

	raw, exists := inputJSON["persona_id"]
	if !exists || raw == nil {
		return Resolution{}
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassPersonaInvalidID,
				Message:    "persona_id invalid",
			},
		}
	}

	personaID, requestedVersion, err := parsePersonaRef(strings.TrimSpace(text))
	if err != nil {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassPersonaInvalidID,
				Message:    "persona_id invalid",
			},
		}
	}

	def, ok := registry.Get(personaID)
	if !ok {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassPersonaNotFound,
				Message:    "persona not found",
				Details:    map[string]any{"persona_id": personaID},
			},
		}
	}

	if requestedVersion != "" && requestedVersion != def.Version {
		return Resolution{
			Error: &ResolutionError{
				ErrorClass: ErrorClassPersonaVersionMismatch,
				Message:    "persona version mismatch",
				Details: map[string]any{
					"persona_id":        personaID,
					"requested_version": requestedVersion,
					"available_version": def.Version,
				},
			},
		}
	}

	role := ""
	if inputJSON != nil {
		if rawRole, ok := inputJSON["role"].(string); ok {
			role = strings.TrimSpace(rawRole)
		}
	}
	resolved, _ := ApplyRoleOverride(def, role)
	return Resolution{Definition: &resolved}
}

func parsePersonaRef(value string) (string, string, error) {
	personaID, version, hasSep := strings.Cut(value, "@")
	if !hasSep {
		return value, "", nil
	}
	personaID = strings.TrimSpace(personaID)
	version = strings.TrimSpace(version)
	if personaID == "" {
		return "", "", fmt.Errorf("persona_id is empty")
	}
	if version == "" {
		return "", "", fmt.Errorf("persona_id@version format missing version")
	}
	return personaID, version, nil
}
