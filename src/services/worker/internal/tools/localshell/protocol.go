package localshell

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ModeBuffered = "buffered"
	ModeFollow   = "follow"
	ModeStdin    = "stdin"
	ModePTY      = "pty"

	StatusRunning    = "running"
	StatusExited     = "exited"
	StatusTerminated = "terminated"
	StatusTimedOut   = "timed_out"
	StatusCancelled  = "cancelled"

	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamPTY    = "pty"
	StreamSystem = "system"

	CodeProcessNotFound       = "process.not_found"
	CodeProcessBusy           = "process.busy"
	CodeNotRunning            = "process.not_running"
	CodeCursorExpired         = "process.cursor_expired"
	CodeInvalidCursor         = "process.invalid_cursor"
	CodeInputSeqRequired      = "process.input_seq_required"
	CodeInputSeqInvalid       = "process.input_seq_invalid"
	CodeStdinNotSupported     = "process.stdin_not_supported"
	CodeCloseStdinUnsupported = "process.close_stdin_unsupported"
	CodeResizeNotSupported    = "process.resize_not_supported"
	CodeInvalidMode           = "process.invalid_mode"
	CodeInvalidSize           = "process.invalid_size"
	CodeTimeoutRequired       = "process.timeout_required"
	CodeTimeoutTooLarge       = "process.timeout_too_large"

	defaultWaitMs          = 500
	defaultBufferedLimit   = 30000
	defaultFollowLimit     = 30000
	maxWaitMs              = 30000
	maxTimeoutMs           = 1800000
	defaultItemBufferBytes = 1 << 20
	defaultResponseBytes   = 32 * 1024
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	return e.Message
}

type Size struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

type ExecCommandRequest struct {
	RunID     string             `json:"-"`
	Command   string             `json:"command"`
	Mode      string             `json:"mode,omitempty"`
	Cwd       string             `json:"cwd,omitempty"`
	TimeoutMs int                `json:"timeout_ms,omitempty"`
	Size      *Size              `json:"size,omitempty"`
	Env       map[string]*string `json:"env,omitempty"`
}

type ContinueProcessRequest struct {
	ProcessRef string  `json:"process_ref"`
	Cursor     string  `json:"cursor"`
	WaitMs     int     `json:"wait_ms,omitempty"`
	StdinText  *string `json:"stdin_text,omitempty"`
	InputSeq   *int64  `json:"input_seq,omitempty"`
	CloseStdin bool    `json:"close_stdin,omitempty"`
}

type TerminateProcessRequest struct {
	ProcessRef string `json:"process_ref"`
}

type ResizeProcessRequest struct {
	ProcessRef string `json:"process_ref"`
	Rows       int    `json:"rows"`
	Cols       int    `json:"cols"`
}

type OutputItem struct {
	Seq    uint64 `json:"seq"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type ArtifactRef struct {
	Key      string `json:"key"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type Response struct {
	Status           string        `json:"status"`
	ProcessRef       string        `json:"process_ref,omitempty"`
	Stdout           string        `json:"stdout,omitempty"`
	Stderr           string        `json:"stderr,omitempty"`
	ExitCode         *int          `json:"exit_code,omitempty"`
	Cursor           string        `json:"cursor,omitempty"`
	NextCursor       string        `json:"next_cursor,omitempty"`
	Items            []OutputItem  `json:"items,omitempty"`
	HasMore          bool          `json:"has_more,omitempty"`
	AcceptedInputSeq *int64        `json:"accepted_input_seq,omitempty"`
	Truncated        bool          `json:"truncated,omitempty"`
	OutputRef        string        `json:"output_ref,omitempty"`
	Artifacts        []ArtifactRef `json:"artifacts,omitempty"`
}

type ItemBuffer struct {
	maxBytes int
	headSeq  uint64
	nextSeq  uint64
	bytes    int
	items    []OutputItem
}

func NewItemBuffer(maxBytes int) *ItemBuffer {
	if maxBytes <= 0 {
		maxBytes = defaultItemBufferBytes
	}
	return &ItemBuffer{maxBytes: maxBytes}
}

func (b *ItemBuffer) Append(stream, text string) {
	if text == "" {
		return
	}
	item := OutputItem{
		Seq:    b.nextSeq,
		Stream: stream,
		Text:   text,
	}
	b.nextSeq++
	b.items = append(b.items, item)
	b.bytes += len(item.Text)
	for b.bytes > b.maxBytes && len(b.items) > 0 {
		b.bytes -= len(b.items[0].Text)
		b.items = b.items[1:]
		b.headSeq++
	}
	if len(b.items) == 0 {
		b.headSeq = b.nextSeq
	}
}

func (b *ItemBuffer) HeadSeq() uint64 {
	return b.headSeq
}

func (b *ItemBuffer) NextSeq() uint64 {
	return b.nextSeq
}

func (b *ItemBuffer) ReadFrom(cursor uint64, limit int) (items []OutputItem, next uint64, hasMore bool, truncated bool, ok bool) {
	if limit <= 0 {
		limit = defaultResponseBytes
	}
	if cursor > b.nextSeq || cursor < b.headSeq {
		return nil, 0, false, false, false
	}
	if cursor == b.nextSeq {
		return nil, b.nextSeq, false, false, true
	}
	start := int(cursor - b.headSeq)
	if start < 0 || start > len(b.items) {
		return nil, 0, false, false, false
	}
	total := 0
	next = cursor
	for i := start; i < len(b.items); i++ {
		item := b.items[i]
		if len(items) > 0 && total+len(item.Text) > limit {
			return items, next, true, true, true
		}
		items = append(items, item)
		total += len(item.Text)
		next = item.Seq + 1
	}
	hasMore = next < b.nextSeq
	return items, next, hasMore, truncated, true
}

func CursorString(seq uint64) string {
	return strconv.FormatUint(seq, 10)
}

func ParseCursor(value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("cursor is required")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cursor must be an unsigned integer")
	}
	return parsed, nil
}

func NormalizeMode(value string) string {
	switch strings.TrimSpace(value) {
	case "":
		return ModeBuffered
	case ModeFollow, ModeStdin, ModePTY:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(value)
	}
}

func NormalizeWaitMs(value int) int {
	if value <= 0 {
		return defaultWaitMs
	}
	if value > maxWaitMs {
		return maxWaitMs
	}
	return value
}

func NormalizeTimeoutMs(mode string, value int) int {
	switch NormalizeMode(mode) {
	case ModeBuffered:
		if value <= 0 {
			return defaultBufferedLimit
		}
	default:
		if value <= 0 {
			return defaultFollowLimit
		}
	}
	if value > maxTimeoutMs {
		return maxTimeoutMs
	}
	return value
}

func ValidateMode(value string) *Error {
	switch strings.TrimSpace(value) {
	case "", ModeBuffered, ModeFollow, ModeStdin, ModePTY:
		return nil
	default:
		return &Error{
			Code:       CodeInvalidMode,
			Message:    fmt.Sprintf("unsupported mode: %s", value),
			HTTPStatus: 400,
		}
	}
}

func ValidateExecRequest(req ExecCommandRequest) *Error {
	mode := NormalizeMode(req.Mode)
	if err := ValidateMode(mode); err != nil {
		return err
	}
	if strings.TrimSpace(req.Command) == "" {
		return &Error{Code: CodeInvalidMode, Message: "command is required", HTTPStatus: 400}
	}
	if mode != ModeBuffered && req.TimeoutMs <= 0 {
		return &Error{Code: CodeTimeoutRequired, Message: "timeout_ms is required for follow, stdin, and pty modes", HTTPStatus: 400}
	}
	if req.TimeoutMs > maxTimeoutMs {
		return &Error{Code: CodeTimeoutTooLarge, Message: "timeout_ms must not exceed 1800000", HTTPStatus: 400}
	}
	if req.Size != nil && (mode != ModePTY || req.Size.Rows <= 0 || req.Size.Cols <= 0) {
		return &Error{Code: CodeInvalidSize, Message: "size is only supported for pty mode and rows/cols must be positive", HTTPStatus: 400}
	}
	return nil
}

func ValidateContinueRequest(req ContinueProcessRequest) *Error {
	if strings.TrimSpace(req.ProcessRef) == "" {
		return &Error{Code: CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}
	if strings.TrimSpace(req.Cursor) == "" {
		return &Error{Code: CodeInvalidCursor, Message: "cursor is invalid", HTTPStatus: 400}
	}
	if req.StdinText != nil {
		if req.InputSeq == nil {
			return &Error{Code: CodeInputSeqRequired, Message: "input_seq is required when stdin_text is provided", HTTPStatus: 400}
		}
		if req.InputSeq != nil && *req.InputSeq <= 0 {
			return &Error{Code: CodeInputSeqInvalid, Message: "input_seq must be positive", HTTPStatus: 400}
		}
	} else if req.InputSeq != nil {
		return &Error{Code: CodeInputSeqInvalid, Message: "input_seq must be positive", HTTPStatus: 400}
	}
	return nil
}

func ValidateResizeRequest(req ResizeProcessRequest) *Error {
	if strings.TrimSpace(req.ProcessRef) == "" {
		return &Error{Code: CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}
	if req.Rows <= 0 || req.Cols <= 0 {
		return &Error{Code: CodeInvalidSize, Message: "size is only supported for pty mode and rows/cols must be positive", HTTPStatus: 400}
	}
	return nil
}
