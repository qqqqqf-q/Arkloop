package http

import (
	"context"
	"encoding/json"
	"strings"

	"arkloop/services/api/internal/data"
	repopersonas "arkloop/services/api/internal/personas"

	"github.com/google/uuid"
)

func findRepoPersonaByKey(repoPersonas []repopersonas.RepoPersona, key string) (*repopersonas.RepoPersona, bool) {
	cleaned := strings.TrimSpace(key)
	if cleaned == "" {
		return nil, false
	}
	for i := range repoPersonas {
		if strings.TrimSpace(repoPersonas[i].ID) == cleaned {
			return &repoPersonas[i], true
		}
	}
	return nil, false
}

func repoPersonaBudgetsJSON(raw map[string]any) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

func repoPersonaExecutorConfigJSON(raw map[string]any) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return encoded
}

func materializeRepoPersonaForCreate(
	ctx context.Context,
	personasRepo *data.PersonasRepository,
	orgID uuid.UUID,
	repoPersona repopersonas.RepoPersona,
	req createPersonaRequest,
) (data.Persona, error) {
	displayName := strings.TrimSpace(repoPersona.Title)
	if trimmed := strings.TrimSpace(req.DisplayName); trimmed != "" {
		displayName = trimmed
	}

	promptMD := strings.TrimSpace(repoPersona.PromptMD)
	if trimmed := strings.TrimSpace(req.PromptMD); trimmed != "" {
		promptMD = trimmed
	}

	description := optionalTrimmedString(repoPersona.Description)
	if req.Description != nil {
		description = optionalTrimmedStringPtr(req.Description)
	}

	toolAllowlist := repoPersona.ToolAllowlist
	if req.ToolAllowlist != nil {
		toolAllowlist = req.ToolAllowlist
	}

	toolDenylist := repoPersona.ToolDenylist
	if req.ToolDenylist != nil {
		toolDenylist = req.ToolDenylist
	}

	budgetsJSON := repoPersonaBudgetsJSON(repoPersona.Budgets)
	if len(req.BudgetsJSON) > 0 {
		budgetsJSON = req.BudgetsJSON
	}

	preferredCredential := optionalTrimmedString(repoPersona.PreferredCredential)
	if req.PreferredCredential != nil {
		preferredCredential = optionalTrimmedStringPtr(req.PreferredCredential)
	}

	model := optionalTrimmedString(repoPersona.Model)
	if req.Model != nil {
		model = optionalTrimmedStringPtr(req.Model)
	}

	reasoningMode := strings.TrimSpace(repoPersona.ReasoningMode)
	if strings.TrimSpace(req.ReasoningMode) != "" {
		reasoningMode = strings.TrimSpace(req.ReasoningMode)
	}

	promptCacheControl := strings.TrimSpace(repoPersona.PromptCacheControl)
	if strings.TrimSpace(req.PromptCacheControl) != "" {
		promptCacheControl = strings.TrimSpace(req.PromptCacheControl)
	}

	executorType := strings.TrimSpace(repoPersona.ExecutorType)
	if strings.TrimSpace(req.ExecutorType) != "" {
		executorType = strings.TrimSpace(req.ExecutorType)
	}

	executorConfigJSON := repoPersonaExecutorConfigJSON(repoPersona.ExecutorConfig)
	if len(req.ExecutorConfigJSON) > 0 {
		executorConfigJSON = req.ExecutorConfigJSON
	}

	return personasRepo.Create(
		ctx,
		orgID,
		repoPersona.ID,
		repoPersona.Version,
		displayName,
		description,
		promptMD,
		toolAllowlist,
		toolDenylist,
		budgetsJSON,
		preferredCredential,
		model,
		reasoningMode,
		promptCacheControl,
		executorType,
		executorConfigJSON,
	)
}

func materializeRepoPersonaForLiteAgent(
	ctx context.Context,
	personasRepo *data.PersonasRepository,
	orgID uuid.UUID,
	repoPersona repopersonas.RepoPersona,
	req createLiteAgentRequest,
) (data.Persona, error) {
	displayName := strings.TrimSpace(repoPersona.Title)
	if trimmed := strings.TrimSpace(req.Name); trimmed != "" {
		displayName = trimmed
	}

	promptMD := strings.TrimSpace(repoPersona.PromptMD)
	if trimmed := strings.TrimSpace(req.PromptMD); trimmed != "" {
		promptMD = trimmed
	}

	toolAllowlist := repoPersona.ToolAllowlist
	if req.ToolAllowlist != nil {
		toolAllowlist = req.ToolAllowlist
	}

	reasoningMode := strings.TrimSpace(repoPersona.ReasoningMode)
	if strings.TrimSpace(req.ReasoningMode) != "" {
		reasoningMode = strings.TrimSpace(req.ReasoningMode)
	}

	executorType := strings.TrimSpace(repoPersona.ExecutorType)
	if strings.TrimSpace(req.ExecutorType) != "" {
		executorType = strings.TrimSpace(req.ExecutorType)
	}

	model := optionalTrimmedString(repoPersona.Model)
	if req.Model != nil {
		model = optionalTrimmedStringPtr(req.Model)
	}

	budgetsJSON := mergeLiteAgentBudgets(repoPersonaBudgetsJSON(repoPersona.Budgets), req.Temperature, req.MaxOutputTokens)

	return personasRepo.Create(
		ctx,
		orgID,
		repoPersona.ID,
		repoPersona.Version,
		displayName,
		optionalTrimmedString(repoPersona.Description),
		promptMD,
		toolAllowlist,
		repoPersona.ToolDenylist,
		budgetsJSON,
		optionalTrimmedString(repoPersona.PreferredCredential),
		model,
		reasoningMode,
		"none",
		executorType,
		repoPersonaExecutorConfigJSON(repoPersona.ExecutorConfig),
	)
}
