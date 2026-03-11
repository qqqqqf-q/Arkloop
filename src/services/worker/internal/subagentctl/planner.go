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
	PersonaID          string
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
	return childRunPlan{
		Mode:             childRunPlanModeCreateRun,
		PersonaID:        personaID,
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
