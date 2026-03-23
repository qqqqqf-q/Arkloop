package subagentctl

import (
	"encoding/json"
	"strings"
	"time"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
)

const (
	MemoryScopeSameUser    = "same_user"
	forkRecentMessageLimit = 12
)

type SpawnInheritRequest struct {
	Messages    *bool
	Attachments *bool
	Workspace   *bool
	Skills      *bool
	Runtime     *bool
	MemoryScope string
	MessageIDs  []uuid.UUID
}

type SpawnParentContext struct {
	ToolAllowlist []string
	ToolDenylist  []string
	RouteID       string
	Model         string
	ProfileRef    string
	WorkspaceRef  string
	EnabledSkills []skillstore.ResolvedSkill
	MemoryScope   string
}

type SpawnRequest struct {
	PersonaID   string
	Role        *string
	Nickname    *string
	ContextMode string
	Inherit     SpawnInheritRequest
	Input       string
	SourceType  string // 默认 "thread_spawn"，platform agent 设为 "platform_agent"
	Profile     string // explore / task / strong

	ParentContext SpawnParentContext
}

type ResolvedSpawnInherit struct {
	Messages    bool     `json:"messages"`
	Attachments bool     `json:"attachments"`
	Workspace   bool     `json:"workspace"`
	Skills      bool     `json:"skills"`
	Runtime     bool     `json:"runtime"`
	MemoryScope string   `json:"memory_scope"`
	MessageIDs  []string `json:"message_ids,omitempty"`
}

type ResolvedSpawnRequest struct {
	PersonaID   string
	Role        *string
	Nickname    *string
	ContextMode string
	Inherit     ResolvedSpawnInherit
	Input       string
	SourceType  string

	ParentContext SpawnParentContext
}

type SendInputRequest struct {
	SubAgentID uuid.UUID
	Input      string
	Interrupt  bool
}

type WaitRequest struct {
	SubAgentIDs []uuid.UUID
	Timeout     time.Duration
}

type ResumeRequest struct {
	SubAgentID uuid.UUID
}

type CloseRequest struct {
	SubAgentID uuid.UUID
}

type InterruptRequest struct {
	SubAgentID uuid.UUID
	Reason     string
}

type StatusSnapshot struct {
	SubAgentID         uuid.UUID  `json:"sub_agent_id"`
	ParentRunID        uuid.UUID  `json:"parent_run_id"`
	RootRunID          uuid.UUID  `json:"root_run_id"`
	Depth              int        `json:"depth"`
	Status             string     `json:"status"`
	Role               *string    `json:"role,omitempty"`
	PersonaID          *string    `json:"persona_id,omitempty"`
	Nickname           *string    `json:"nickname,omitempty"`
	ContextMode        string     `json:"context_mode,omitempty"`
	CurrentRunID       *uuid.UUID `json:"current_run_id,omitempty"`
	LastCompletedRunID *uuid.UUID `json:"last_completed_run_id,omitempty"`
	LastOutputRef      *string    `json:"last_output_ref,omitempty"`
	LastOutput         *string    `json:"output,omitempty"`
	LastError          *string    `json:"last_error,omitempty"`
	LastEventSeq       *int64     `json:"last_event_seq,omitempty"`
	LastEventType      *string    `json:"last_event_type,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	Degraded           bool       `json:"degraded,omitempty"`
}

type ContextSnapshot struct {
	ContextMode string                     `json:"context_mode"`
	Inherit     ResolvedSpawnInherit       `json:"inherit"`
	Messages    []ContextSnapshotMessage   `json:"messages,omitempty"`
	Environment ContextSnapshotEnvironment `json:"environment"`
	Skills      []skillstore.ResolvedSkill `json:"skills,omitempty"`
	Routing     *ContextSnapshotRouting    `json:"routing,omitempty"`
	Runtime     ContextSnapshotRuntime     `json:"runtime"`
	Memory      ContextSnapshotMemory      `json:"memory"`
}

type ContextSnapshotMessage struct {
	SourceMessageID string          `json:"source_message_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentJSON     json.RawMessage `json:"content_json,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ContextSnapshotEnvironment struct {
	ProfileRef   string `json:"profile_ref,omitempty"`
	WorkspaceRef string `json:"workspace_ref,omitempty"`
}

type ContextSnapshotRouting struct {
	RouteID string `json:"route_id,omitempty"`
	Model   string `json:"model,omitempty"`
}

type ContextSnapshotRuntime struct {
	ToolAllowlist  []string `json:"tool_allowlist,omitempty"`
	ToolDenylist   []string `json:"tool_denylist,omitempty"`
	RouteID        string   `json:"route_id,omitempty"`
	Model          string   `json:"model,omitempty"`
	ApprovalPolicy string   `json:"approval_policy,omitempty"`
	SandboxPolicy  string   `json:"sandbox_policy,omitempty"`
}

type ContextSnapshotMemory struct {
	Scope string `json:"scope"`
}

func (s ContextSnapshot) EffectiveRouting() ContextSnapshotRouting {
	routeID := strings.TrimSpace(s.Runtime.RouteID)
	model := strings.TrimSpace(s.Runtime.Model)
	if s.Routing != nil {
		if routeID == "" {
			routeID = strings.TrimSpace(s.Routing.RouteID)
		}
		if model == "" {
			model = strings.TrimSpace(s.Routing.Model)
		}
	}
	return ContextSnapshotRouting{
		RouteID: routeID,
		Model:   model,
	}
}
