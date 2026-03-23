package shell

import "arkloop/services/shared/skillstore"

const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusClosed  = "closed"

	defaultYieldTimeMs = 1000
	maxYieldTimeMs     = 30000
	maxTimeoutMs       = 1800000
)

type ExecCommandRequest struct {
	SessionID     string                     `json:"session_id"`
	OpenMode      string                     `json:"open_mode,omitempty"`
	AccountID         string                     `json:"account_id,omitempty"`
	ProfileRef    string                     `json:"profile_ref,omitempty"`
	WorkspaceRef  string                     `json:"workspace_ref,omitempty"`
	EnabledSkills []skillstore.ResolvedSkill `json:"enabled_skills,omitempty"`
	Tier          string                     `json:"tier,omitempty"`
	Cwd           string                     `json:"cwd,omitempty"`
	Command       string                     `json:"command"`
	TimeoutMs     int                        `json:"timeout_ms,omitempty"`
	YieldTimeMs   int                        `json:"yield_time_ms,omitempty"`
	Env           map[string]string          `json:"env,omitempty"`
}

type WriteStdinRequest struct {
	SessionID   string `json:"session_id"`
	AccountID       string `json:"account_id,omitempty"`
	Chars       string `json:"chars,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type ForkSessionRequest struct {
	AccountID         string `json:"account_id,omitempty"`
	FromSessionID string `json:"from_session_id"`
	ToSessionID   string `json:"to_session_id"`
}

type ArtifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type Response struct {
	SessionID       string        `json:"session_id"`
	Status          string        `json:"status"`
	Cwd             string        `json:"cwd"`
	Output          string        `json:"output"`
	Running         bool          `json:"running"`
	Truncated       bool          `json:"truncated"`
	TimedOut        bool          `json:"timed_out"`
	ExitCode        *int          `json:"exit_code,omitempty"`
	Artifacts       []ArtifactRef `json:"artifacts,omitempty"`
	Restored        bool          `json:"restored,omitempty"`
	RestoreRevision string        `json:"restore_revision,omitempty"`
}

type ForkSessionResponse struct {
	RestoreRevision string `json:"restore_revision,omitempty"`
}

type DebugTranscript struct {
	Text         string `json:"text"`
	Truncated    bool   `json:"truncated"`
	OmittedBytes int64  `json:"omitted_bytes"`
}

type DebugResponse struct {
	SessionID              string          `json:"session_id"`
	Status                 string          `json:"status"`
	Cwd                    string          `json:"cwd"`
	Running                bool            `json:"running"`
	TimedOut               bool            `json:"timed_out"`
	ExitCode               *int            `json:"exit_code,omitempty"`
	PendingOutputBytes     int             `json:"pending_output_bytes"`
	PendingOutputTruncated bool            `json:"pending_output_truncated"`
	Transcript             DebugTranscript `json:"transcript"`
	Tail                   string          `json:"tail"`
}

type AgentRequest struct {
	Action      string                   `json:"action"`
	ExecCommand *AgentExecCommandRequest `json:"exec_command,omitempty"`
	WriteStdin  *AgentWriteStdinRequest  `json:"write_stdin,omitempty"`
	State       *AgentStateRequest       `json:"state,omitempty"`
}

type AgentExecCommandRequest struct {
	Cwd         string            `json:"cwd,omitempty"`
	Command     string            `json:"command"`
	TimeoutMs   int               `json:"timeout_ms,omitempty"`
	YieldTimeMs int               `json:"yield_time_ms,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

type AgentWriteStdinRequest struct {
	Chars       string `json:"chars,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type AgentStateRequest struct{}

type AgentStateResponse struct {
	Cwd string            `json:"cwd,omitempty"`
	Env map[string]string `json:"env,omitempty"`
}

type AgentResponse struct {
	Action     string                `json:"action"`
	Session    *AgentSessionResponse `json:"session,omitempty"`
	Debug      *AgentDebugResponse   `json:"debug,omitempty"`
	State      *AgentStateResponse   `json:"state,omitempty"`
	ExecOutput *OutputDeltasResponse `json:"exec_output,omitempty"`
	Code       string                `json:"code,omitempty"`
	Error      string                `json:"error,omitempty"`
}

type AgentSessionResponse struct {
	Status    string `json:"status"`
	Cwd       string `json:"cwd"`
	Output    string `json:"output"`
	Running   bool   `json:"running"`
	Truncated bool   `json:"truncated"`
	TimedOut  bool   `json:"timed_out"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type AgentDebugResponse struct {
	Status                 string          `json:"status"`
	Cwd                    string          `json:"cwd"`
	Running                bool            `json:"running"`
	TimedOut               bool            `json:"timed_out"`
	ExitCode               *int            `json:"exit_code,omitempty"`
	PendingOutputBytes     int             `json:"pending_output_bytes"`
	PendingOutputTruncated bool            `json:"pending_output_truncated"`
	Transcript             DebugTranscript `json:"transcript"`
	Tail                   string          `json:"tail"`
}

// OutputDeltasResponse carries output deltas from a running command session.
type OutputDeltasResponse struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Running bool   `json:"running"`
}

func NormalizeYieldTimeMs(value int) int {
	if value <= 0 {
		return defaultYieldTimeMs
	}
	if value > maxYieldTimeMs {
		return maxYieldTimeMs
	}
	return value
}

func NormalizeTimeoutMs(value int) int {
	if value <= 0 {
		return 30_000
	}
	return value
}

func NormalizeOpenMode(value string) string {
	switch value {
	case OpenModeAttachOrRestore:
		return OpenModeAttachOrRestore
	default:
		return OpenModeCreate
	}
}

func ValidateTimeoutMs(value int) *Error {
	if value > maxTimeoutMs {
		return timeoutTooLargeError()
	}
	return nil
}
