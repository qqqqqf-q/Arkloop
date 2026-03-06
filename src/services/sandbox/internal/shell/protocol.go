package shell

const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusClosed  = "closed"

	defaultYieldTimeMs = 1000
	maxYieldTimeMs     = 30000
	maxTimeoutMs       = 300000
)

type ExecCommandRequest struct {
	SessionID   string `json:"session_id"`
	OrgID       string `json:"org_id,omitempty"`
	Tier        string `json:"tier,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Command     string `json:"command"`
	TimeoutMs   int    `json:"timeout_ms,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type WriteStdinRequest struct {
	SessionID   string `json:"session_id"`
	OrgID       string `json:"org_id,omitempty"`
	Chars       string `json:"chars,omitempty"`
	YieldTimeMs int    `json:"yield_time_ms,omitempty"`
}

type ArtifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type Response struct {
	SessionID string        `json:"session_id"`
	Status    string        `json:"status"`
	Cwd       string        `json:"cwd"`
	Output    string        `json:"output"`
	Running   bool          `json:"running"`
	Truncated bool          `json:"truncated"`
	TimedOut  bool          `json:"timed_out"`
	ExitCode  *int          `json:"exit_code,omitempty"`
	Artifacts []ArtifactRef `json:"artifacts,omitempty"`
}

type AgentRequest struct {
	Action      string                   `json:"action"`
	ExecCommand *AgentExecCommandRequest `json:"exec_command,omitempty"`
	WriteStdin  *AgentWriteStdinRequest  `json:"write_stdin,omitempty"`
	Checkpoint  *AgentCheckpointRequest  `json:"checkpoint,omitempty"`
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

type AgentCheckpointRequest struct {
	Archive string `json:"archive,omitempty"`
}

type AgentCheckpointResponse struct {
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Archive string            `json:"archive,omitempty"`
}

type AgentResponse struct {
	Action     string                   `json:"action"`
	Session    *AgentSessionResponse    `json:"session,omitempty"`
	Checkpoint *AgentCheckpointResponse `json:"checkpoint,omitempty"`
	Code       string                   `json:"code,omitempty"`
	Error      string                   `json:"error,omitempty"`
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

func ValidateTimeoutMs(value int) *Error {
	if value > maxTimeoutMs {
		return timeoutTooLargeError()
	}
	return nil
}
