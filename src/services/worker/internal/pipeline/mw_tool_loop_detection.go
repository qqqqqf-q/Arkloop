package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

const (
	loopWindowSize       = 30
	loopWarnThreshold    = 5
	loopBlockThreshold   = 10
	loopNoProgressThresh = 8
	loopPingPongThresh   = 6
)

// volatile fields excluded from hash
var hashExcludeKeys = map[string]struct{}{
	"timestamp":    {},
	"random":       {},
	"nonce":        {},
	"request_id":   {},
	"trace_id":     {},
	"tool_call_id": {},
	"id":           {},
	"call_id":      {},
	"tool_use_id":  {},
}

type toolCallRecord struct {
	hash       string
	resultHash string
	toolName   string
}

// ToolLoopDetector tracks recent tool calls and detects repetitive patterns.
type ToolLoopDetector struct {
	mu     sync.Mutex
	window []toolCallRecord
}

func newToolLoopDetector() *ToolLoopDetector {
	return &ToolLoopDetector{
		window: make([]toolCallRecord, 0, loopWindowSize),
	}
}

func (d *ToolLoopDetector) record(toolName string, paramsJSON map[string]any, resultHash string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	h := computeCallHash(toolName, paramsJSON)
	rec := toolCallRecord{hash: h, resultHash: resultHash, toolName: toolName}
	d.window = append(d.window, rec)
	if len(d.window) > loopWindowSize {
		d.window = d.window[len(d.window)-loopWindowSize:]
	}
}

type loopDetection struct {
	Level   string // "warning" or "block"
	Message string
}

func (d *ToolLoopDetector) check(toolName string, paramsJSON map[string]any) *loopDetection {
	d.mu.Lock()
	defer d.mu.Unlock()

	h := computeCallHash(toolName, paramsJSON)

	// rule 1: same tool+params repeated
	count := 0
	for _, rec := range d.window {
		if rec.hash == h {
			count++
		}
	}
	if count >= loopBlockThreshold {
		return &loopDetection{
			Level:   "block",
			Message: fmt.Sprintf("You have called %s with identical parameters %d times. This call is blocked. Try a different approach or different parameters.", toolName, count),
		}
	}
	if count >= loopWarnThreshold {
		return &loopDetection{
			Level:   "warning",
			Message: fmt.Sprintf("You have called %s with identical parameters %d times already. Consider changing your approach or using different parameters.", toolName, count),
		}
	}

	// rule 2: no-progress detection (same result hash consecutively)
	if len(d.window) >= loopNoProgressThresh {
		tail := d.window[len(d.window)-loopNoProgressThresh:]
		allSame := true
		for _, rec := range tail[1:] {
			if rec.resultHash != tail[0].resultHash || rec.resultHash == "" {
				allSame = false
				break
			}
		}
		if allSame {
			return &loopDetection{
				Level:   "warning",
				Message: fmt.Sprintf("The last %d tool calls produced identical results with no progress. Consider a fundamentally different approach.", loopNoProgressThresh),
			}
		}
	}

	// rule 3: A-B-A-B ping-pong
	if len(d.window) >= loopPingPongThresh-1 {
		tail := d.window[len(d.window)-(loopPingPongThresh-1):]
		isPingPong := true
		for i := 2; i < len(tail); i++ {
			if tail[i].hash != tail[i-2].hash {
				isPingPong = false
				break
			}
		}
		if isPingPong && tail[0].hash != tail[1].hash && (h == tail[0].hash || h == tail[1].hash) {
			return &loopDetection{
				Level:   "warning",
				Message: fmt.Sprintf("Detected alternating pattern between %s and %s. Break the cycle by trying a different strategy.", tail[0].toolName, tail[1].toolName),
			}
		}
	}

	return nil
}

func computeCallHash(toolName string, params map[string]any) string {
	filtered := filterParams(params)
	data, _ := json.Marshal(filtered)
	sum := sha256.Sum256(append([]byte(toolName+"\x00"), data...))
	return hex.EncodeToString(sum[:16])
}

func filterParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		if _, excluded := hashExcludeKeys[k]; !excluded {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		out[k] = params[k]
	}
	return out
}

// NewToolLoopDetectionMiddleware creates a middleware that detects and prevents tool call loops.
// Position: after ToolBuildMiddleware, before AgentLoopHandler.
func NewToolLoopDetectionMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		detector := newToolLoopDetector()
		rc.ToolLoopDetector = detector
		return next(ctx, rc)
	}
}

// CheckToolLoop should be called by the agent loop before executing a tool.
// Returns a warning message to inject, or empty string. If block is true, the call should be refused.
func CheckToolLoop(rc *RunContext, toolName string, args map[string]any) (message string, block bool) {
	if rc == nil || rc.ToolLoopDetector == nil {
		return "", false
	}
	det := rc.ToolLoopDetector.check(toolName, args)
	if det == nil {
		return "", false
	}
	slog.Warn("tool loop detected",
		"tool", toolName,
		"level", det.Level,
		"run_id", rc.Run.ID,
	)
	return det.Message, det.Level == "block"
}

// RecordToolCall records a completed tool call for loop detection.
func RecordToolCall(rc *RunContext, toolName string, args map[string]any, resultJSON map[string]any) {
	if rc == nil || rc.ToolLoopDetector == nil {
		return
	}
	var resultHash string
	if resultJSON != nil {
		data, _ := json.Marshal(resultJSON)
		sum := sha256.Sum256(data)
		resultHash = hex.EncodeToString(sum[:8])
	}
	rc.ToolLoopDetector.record(toolName, args, resultHash)
}

// loopWarningBlock formats a system-level warning block for injection.
func FormatLoopWarning(message string) string {
	return fmt.Sprintf("\n<tool_loop_warning>\n%s\n</tool_loop_warning>\n", strings.TrimSpace(message))
}
