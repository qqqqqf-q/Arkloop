package subagentctl

import (
	"fmt"
	"strings"

	"arkloop/services/worker/internal/data"
)

type childRunPlanMode string

const (
	childRunPlanModeCreateRun childRunPlanMode = "create_run"
	childRunPlanModeQueue     childRunPlanMode = "queue"
)

type childRunPlan struct {
	Mode               childRunPlanMode
	Spawn              *ResolvedSpawnRequest
	Input              string
	Priority           bool
	InterruptActiveRun bool
	PrimaryEventType   string
}

type ChildRunPlanner struct{}

func NewChildRunPlanner() *ChildRunPlanner {
	return &ChildRunPlanner{}
}

func (p *ChildRunPlanner) PlanSpawn(req SpawnRequest) (childRunPlan, error) {
	personaID := strings.TrimSpace(req.PersonaID)
	if personaID == "" {
		return childRunPlan{}, fmt.Errorf("persona_id must be a non-empty string")
	}
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return childRunPlan{}, fmt.Errorf("input must be a non-empty string")
	}
	contextMode := strings.TrimSpace(req.ContextMode)
	switch contextMode {
	case data.SubAgentContextModeIsolated,
		data.SubAgentContextModeForkRecent,
		data.SubAgentContextModeForkThread,
		data.SubAgentContextModeForkSelected,
		data.SubAgentContextModeSharedWorkspaceOnly:
	default:
		return childRunPlan{}, fmt.Errorf("invalid sub_agent context_mode: %s", contextMode)
	}

	resolved, err := resolveSpawnRequest(req)
	if err != nil {
		return childRunPlan{}, err
	}
	resolved.PersonaID = personaID
	resolved.Input = input
	resolved.ContextMode = contextMode
	resolved.Role = normalizeOptionalString(req.Role)
	resolved.Nickname = normalizeOptionalString(req.Nickname)
	resolved.ParentContext = req.ParentContext
	resolved.SourceType = strings.TrimSpace(req.SourceType)
	if resolved.SourceType == "" {
		resolved.SourceType = data.SubAgentSourceTypeThreadSpawn
	}
	return childRunPlan{
		Mode:             childRunPlanModeCreateRun,
		Spawn:            &resolved,
		Input:            input,
		PrimaryEventType: data.SubAgentEventTypeSpawned,
	}, nil
}

func (p *ChildRunPlanner) PlanSendInput(record data.SubAgentRecord, req SendInputRequest) (childRunPlan, error) {
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return childRunPlan{}, fmt.Errorf("input must not be empty")
	}
	switch record.Status {
	case data.SubAgentStatusClosed:
		return childRunPlan{}, fmt.Errorf("send_input not allowed for sub_agent status: %s", record.Status)
	case data.SubAgentStatusCompleted,
		data.SubAgentStatusFailed,
		data.SubAgentStatusCancelled,
		data.SubAgentStatusResumable,
		data.SubAgentStatusWaitingInput:
		return childRunPlan{
			Mode:             childRunPlanModeCreateRun,
			Input:            input,
			PrimaryEventType: data.SubAgentEventTypeInputSent,
		}, nil
	case data.SubAgentStatusQueued, data.SubAgentStatusRunning:
		return childRunPlan{
			Mode:               childRunPlanModeQueue,
			Input:              input,
			Priority:           req.Interrupt,
			InterruptActiveRun: req.Interrupt,
			PrimaryEventType:   data.SubAgentEventTypeInputSent,
		}, nil
	default:
		return childRunPlan{}, fmt.Errorf("send_input not allowed for sub_agent status: %s", record.Status)
	}
}

func (p *ChildRunPlanner) PlanResume(record data.SubAgentRecord) (childRunPlan, error) {
	switch record.Status {
	case data.SubAgentStatusCompleted,
		data.SubAgentStatusFailed,
		data.SubAgentStatusCancelled,
		data.SubAgentStatusResumable:
		return childRunPlan{Mode: childRunPlanModeCreateRun, PrimaryEventType: data.SubAgentEventTypeResumed}, nil
	default:
		return childRunPlan{}, fmt.Errorf("resume not allowed for sub_agent status: %s", record.Status)
	}
}

func resolveSpawnRequest(req SpawnRequest) (ResolvedSpawnRequest, error) {
	resolved := ResolvedSpawnRequest{
		Inherit: ResolvedSpawnInherit{
			MemoryScope: MemoryScopeSameUser,
		},
	}
	switch strings.TrimSpace(req.ContextMode) {
	case data.SubAgentContextModeIsolated:
		resolved.Inherit.Messages = false
		resolved.Inherit.Attachments = false
		resolved.Inherit.Workspace = false
		resolved.Inherit.Skills = false
		resolved.Inherit.Runtime = false
	case data.SubAgentContextModeForkRecent, data.SubAgentContextModeForkThread:
		resolved.Inherit.Messages = true
		resolved.Inherit.Attachments = true
		resolved.Inherit.Workspace = true
		resolved.Inherit.Skills = true
		resolved.Inherit.Runtime = true
	case data.SubAgentContextModeForkSelected:
		resolved.Inherit.Messages = true
		resolved.Inherit.Attachments = true
		resolved.Inherit.Workspace = true
		resolved.Inherit.Skills = true
		resolved.Inherit.Runtime = true
	case data.SubAgentContextModeSharedWorkspaceOnly:
		resolved.Inherit.Messages = false
		resolved.Inherit.Attachments = false
		resolved.Inherit.Workspace = true
		resolved.Inherit.Skills = true
		resolved.Inherit.Runtime = false
	default:
		return ResolvedSpawnRequest{}, fmt.Errorf("invalid sub_agent context_mode: %s", req.ContextMode)
	}

	if raw := strings.TrimSpace(req.Inherit.MemoryScope); raw != "" {
		if raw != MemoryScopeSameUser {
			return ResolvedSpawnRequest{}, fmt.Errorf("inherit.memory_scope must be same_user")
		}
		resolved.Inherit.MemoryScope = raw
	}
	if req.Inherit.Messages != nil {
		resolved.Inherit.Messages = *req.Inherit.Messages
	}
	if req.Inherit.Attachments != nil {
		resolved.Inherit.Attachments = *req.Inherit.Attachments
	}
	if req.Inherit.Workspace != nil {
		resolved.Inherit.Workspace = *req.Inherit.Workspace
	}
	if req.Inherit.Skills != nil {
		resolved.Inherit.Skills = *req.Inherit.Skills
	}
	if req.Inherit.Runtime != nil {
		resolved.Inherit.Runtime = *req.Inherit.Runtime
	}
	for _, id := range req.Inherit.MessageIDs {
		resolved.Inherit.MessageIDs = append(resolved.Inherit.MessageIDs, id.String())
	}

	if resolved.Inherit.Attachments && !resolved.Inherit.Messages {
		return ResolvedSpawnRequest{}, fmt.Errorf("inherit.attachments requires inherit.messages")
	}
	if resolved.Inherit.Skills && !resolved.Inherit.Workspace {
		return ResolvedSpawnRequest{}, fmt.Errorf("inherit.skills requires inherit.workspace")
	}

	switch strings.TrimSpace(req.ContextMode) {
	case data.SubAgentContextModeIsolated:
		if resolved.Inherit.Messages || resolved.Inherit.Attachments || resolved.Inherit.Workspace || resolved.Inherit.Skills || resolved.Inherit.Runtime {
			return ResolvedSpawnRequest{}, fmt.Errorf("isolated context_mode does not allow inherited context")
		}
	case data.SubAgentContextModeForkRecent, data.SubAgentContextModeForkThread:
		if !resolved.Inherit.Messages {
			return ResolvedSpawnRequest{}, fmt.Errorf("%s context_mode requires inherit.messages=true", strings.TrimSpace(req.ContextMode))
		}
	case data.SubAgentContextModeForkSelected:
		if !resolved.Inherit.Messages {
			return ResolvedSpawnRequest{}, fmt.Errorf("fork_selected context_mode requires inherit.messages=true")
		}
		if len(resolved.Inherit.MessageIDs) == 0 {
			return ResolvedSpawnRequest{}, fmt.Errorf("fork_selected requires inherit.message_ids")
		}
	case data.SubAgentContextModeSharedWorkspaceOnly:
		if resolved.Inherit.Messages || resolved.Inherit.Attachments || resolved.Inherit.Runtime {
			return ResolvedSpawnRequest{}, fmt.Errorf("shared_workspace_only only allows workspace and skills inheritance")
		}
		if !resolved.Inherit.Workspace {
			return ResolvedSpawnRequest{}, fmt.Errorf("shared_workspace_only requires inherit.workspace=true")
		}
	}
	return resolved, nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
