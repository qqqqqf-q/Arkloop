package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/llm"
)

func NewPromptHookMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ImpressionRun || isImpressionRun(rc) || rc.StickerRegisterRun || isStickerRegisterRun(rc) {
			return next(ctx, rc)
		}
		rc.SetBaseUserMessages(collectTrailingRealUserMessages(rc.Messages, rc.ThreadMessageIDs))
		if rc.HookRuntime == nil {
			return next(ctx, rc)
		}
		if rc.ResumePromptSnapshot != nil {
			rc.ApplyResumePromptSnapshot()
			return next(ctx, rc)
		}
		rc.RemovePromptSegmentsByPrefix("hook.before.")
		rc.RemovePromptSegmentsByPrefix("hook.after.")
		appendHookPromptSegments(rc, rc.HookRuntime.BeforePromptSegments(ctx, rc, "hook.before"))
		assembledPrompt := rc.MaterializedSystemPrompt()
		appendHookPromptSegments(rc, rc.HookRuntime.AfterPromptSegments(ctx, rc, assembledPrompt, "hook.after"))
		return next(ctx, rc)
	}
}

func NewThreadPersistHookMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		err := next(ctx, rc)
		if rc == nil || rc.HookRuntime == nil || !rc.ThreadPersistReady {
			return err
		}
		if rc.ImpressionRun || isImpressionRun(rc) || rc.StickerRegisterRun || isStickerRegisterRun(rc) {
			return err
		}
		delta := BuildThreadDelta(rc)
		beforeHints := rc.HookRuntime.BeforeThreadPersist(ctx, rc, delta)
		result := rc.HookRuntime.ExecuteThreadPersist(ctx, rc, delta, beforeHints)
		rc.HookRuntime.AfterThreadPersist(ctx, rc, delta, result)
		return err
	}
}

func BuildThreadDelta(rc *RunContext) ThreadDelta {
	if rc == nil {
		return ThreadDelta{}
	}
	messages := make([]ThreadDeltaMessage, 0, len(rc.baseUserMessages)+len(rc.runtimeUserMessages))
	for _, msg := range rc.baseUserMessages {
		messages = append(messages, ThreadDeltaMessage{Role: msg.Role, Content: msg.Content})
	}
	for _, msg := range rc.runtimeUserMessages {
		messages = append(messages, ThreadDeltaMessage{Role: msg.Role, Content: msg.Content})
	}
	delta := ThreadDelta{
		RunID:           rc.Run.ID,
		ThreadID:        rc.Run.ThreadID,
		AccountID:       rc.Run.AccountID,
		AgentID:         StableAgentID(rc),
		Messages:        messages,
		AssistantOutput: rc.FinalAssistantOutput,
		ToolCallCount:   rc.RunToolCallCount,
		IterationCount:  rc.RunIterationCount,
		TraceID:         rc.TraceID,
	}
	if rc.UserID != nil {
		delta.UserID = *rc.UserID
	}
	return delta
}

func buildCompactHintsForRun(ctx context.Context, rc *RunContext, input CompactInput) string {
	if rc == nil || rc.HookRuntime == nil {
		return ""
	}
	return BuildCompactHintsBlock(rc.HookRuntime.BeforeCompact(ctx, rc, input))
}

func compactSystemPromptForRun(ctx context.Context, rc *RunContext, systemPrompt string, messages []llm.Message) string {
	prompt := strings.TrimSpace(systemPrompt)
	if hintBlock := buildCompactHintsForRun(ctx, rc, CompactInput{
		SystemPrompt: prompt,
		Messages:     append([]llm.Message(nil), messages...),
	}); hintBlock != "" {
		if prompt == "" {
			return hintBlock
		}
		return prompt + "\n\n" + hintBlock
	}
	return prompt
}

func notifyCompactApplied(ctx context.Context, rc *RunContext, input CompactInput, output CompactOutput) {
	if rc == nil || rc.HookRuntime == nil {
		return
	}
	if strings.TrimSpace(output.SystemPrompt) == "" {
		output.SystemPrompt = input.SystemPrompt
	}
	rc.HookRuntime.AfterCompact(ctx, rc, output)
}

func appendHookPromptSegments(rc *RunContext, segments PromptSegments) {
	if rc == nil || len(segments) == 0 {
		return
	}
	for _, segment := range segments {
		rc.AppendPromptSegment(segment)
	}
}

func normalizeHookPromptSegments(segmentPrefix string, segments PromptSegments) PromptSegments {
	if len(segments) == 0 {
		return nil
	}
	out := make(PromptSegments, 0, len(segments))
	for i, segment := range segments {
		normalized := segment
		if strings.TrimSpace(normalized.Name) == "" {
			normalized.Name = fmt.Sprintf("%s.%03d.segment", strings.TrimSpace(segmentPrefix), i)
		}
		out = append(out, normalized)
	}
	return out
}

func sanitizePromptSegmentName(name string) string {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "segment"
	}
	replacer := strings.NewReplacer("/", "_", " ", "_", "\t", "_", "\n", "_")
	return replacer.Replace(cleaned)
}
