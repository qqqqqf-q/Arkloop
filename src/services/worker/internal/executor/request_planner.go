package executor

import (
	"strings"

	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
)

type promptPlanMode string

const (
	promptPlanModeFull        promptPlanMode = "full"
	promptPlanModeRuntimeTail promptPlanMode = "runtime_tail"
	promptPlanModeNone        promptPlanMode = "none"
)

type requestPlannerInput struct {
	Model            string
	BaseMessages     []llm.Message
	PromptMode       promptPlanMode
	Tools            []llm.ToolSpec
	MaxOutputTokens  *int
	Temperature      *float64
	ReasoningMode    string
	ToolChoice       *llm.ToolChoice
	ApplyImageFilter bool
}

type plannedRequest struct {
	Request           llm.Request
	CacheSafeSnapshot *agent.CacheSafeSnapshot
}

type promptSegment struct {
	Name          string
	Target        string
	Role          string
	Text          string
	Stability     string
	CacheEligible bool
}

func planRequestFromRunContext(rc *pipeline.RunContext, input requestPlannerInput) plannedRequest {
	baseMessages := append([]llm.Message(nil), input.BaseMessages...)
	if input.ApplyImageFilter {
		baseMessages = applyImageFilter(rc.SelectedRoute, baseMessages, rc.ReadCapabilities.ReadImageSourcesVisible)
	}
	tools := annotateToolCacheHints(rc, input.Tools)
	if request, snapshot, ok := inheritedPromptCacheRequest(rc, input, baseMessages, tools); ok {
		return plannedRequest{
			Request:           request,
			CacheSafeSnapshot: snapshot,
		}
	}
	messages := applyPromptPlan(rc, baseMessages, input.PromptMode)

	req := llm.Request{
		Model:           input.Model,
		Messages:        messages,
		Tools:           tools,
		MaxOutputTokens: cloneIntPtr(input.MaxOutputTokens),
		Temperature:     cloneFloatPtr(input.Temperature),
		ReasoningMode:   strings.TrimSpace(input.ReasoningMode),
		ToolChoice:      cloneToolChoice(input.ToolChoice),
		PromptPlan:      buildPromptPlan(rc, input.PromptMode, messages),
	}

	return plannedRequest{
		Request:           req,
		CacheSafeSnapshot: buildCacheSafeSnapshot(rc, baseMessages, req),
	}
}

func applyPromptPlan(rc *pipeline.RunContext, baseMessages []llm.Message, mode promptPlanMode) []llm.Message {
	switch mode {
	case promptPlanModeNone:
		return baseMessages
	case promptPlanModeRuntimeTail:
		out, _ := applyRuntimeTailFromAssembly(rc, baseMessages)
		return out
	default:
		out, _ := applyFullPromptFromAssembly(rc, baseMessages)
		return out
	}
}

func applyFullPromptFromAssembly(rc *pipeline.RunContext, baseMessages []llm.Message) ([]llm.Message, bool) {
	segments, ok := promptSegmentsFromRunContext(rc)
	if !ok || len(segments) == 0 {
		return append([]llm.Message(nil), baseMessages...), false
	}

	prefix := make([]llm.Message, 0, len(segments))
	tail := make([]llm.Message, 0, len(segments))
	for _, seg := range segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		target := strings.ToLower(strings.TrimSpace(seg.Target))
		role := normalizePromptSegmentRole(seg.Role, target)
		part := llm.TextPart{Text: text}
		if shouldCachePromptSegment(seg, target, role, rc) {
			ephemeral := "ephemeral"
			part.CacheControl = &ephemeral
		}
		msg := llm.Message{Role: role, Content: []llm.TextPart{part}}
		if target == string(pipeline.PromptTargetRuntimeTail) {
			tail = append(tail, msg)
			continue
		}
		prefix = append(prefix, msg)
	}
	out := append(prefix, baseMessages...)
	out = append(out, tail...)
	return out, true
}

func applyRuntimeTailFromAssembly(rc *pipeline.RunContext, baseMessages []llm.Message) ([]llm.Message, bool) {
	segments, ok := promptSegmentsFromRunContext(rc)
	if !ok || len(segments) == 0 {
		return append([]llm.Message(nil), baseMessages...), false
	}
	out := append([]llm.Message(nil), baseMessages...)
	appended := false
	for _, seg := range segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(seg.Target)) != string(pipeline.PromptTargetRuntimeTail) {
			continue
		}
		role := normalizePromptSegmentRole(seg.Role, string(pipeline.PromptTargetRuntimeTail))
		out = append(out, llm.Message{
			Role:    role,
			Content: []llm.TextPart{{Text: text}},
		})
		appended = true
	}
	if !appended {
		return append([]llm.Message(nil), baseMessages...), false
	}
	return out, true
}

func normalizePromptSegmentRole(rawRole string, target string) string {
	role := strings.ToLower(strings.TrimSpace(rawRole))
	switch role {
	case "system", "user", "assistant", "tool":
		return role
	}
	if target == "system_prefix" {
		return "system"
	}
	return "user"
}

func shouldCachePromptSegment(seg promptSegment, target string, role string, rc *pipeline.RunContext) bool {
	if seg.CacheEligible {
		return true
	}
	if target != "system_prefix" || role != "system" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(seg.Stability), "volatile_tail") {
		return false
	}
	return promptCacheEnabled(rc)
}

func promptCacheEnabled(rc *pipeline.RunContext) bool {
	return rc != nil && rc.AgentConfig != nil && strings.TrimSpace(rc.AgentConfig.PromptCacheControl) == "system_prompt"
}

func promptSegmentsFromRunContext(rc *pipeline.RunContext) ([]promptSegment, bool) {
	if rc == nil || len(rc.PromptAssembly.Segments) == 0 {
		return nil, false
	}
	out := make([]promptSegment, 0, len(rc.PromptAssembly.Segments))
	for _, item := range rc.PromptAssembly.Segments {
		out = append(out, promptSegment{
			Name:          strings.TrimSpace(item.Name),
			Target:        strings.TrimSpace(string(item.Target)),
			Role:          strings.TrimSpace(item.Role),
			Text:          strings.TrimSpace(item.Text),
			Stability:     strings.TrimSpace(string(item.Stability)),
			CacheEligible: item.CacheEligible,
		})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func buildPromptPlan(rc *pipeline.RunContext, mode promptPlanMode, messages []llm.Message) *llm.PromptPlan {
	if mode == promptPlanModeNone {
		return nil
	}
	plan := &llm.PromptPlan{}
	if segments, ok := promptSegmentsFromRunContext(rc); ok {
		for _, seg := range segments {
			target := strings.ToLower(strings.TrimSpace(seg.Target))
			role := normalizePromptSegmentRole(seg.Role, target)
			block := llm.PromptPlanBlock{
				Name:          strings.TrimSpace(seg.Name),
				Target:        target,
				Role:          role,
				Text:          strings.TrimSpace(seg.Text),
				Stability:     normalizePromptStability(seg.Stability),
				CacheEligible: shouldCachePromptSegment(seg, target, role, rc),
			}
			if role == "system" && (target == string(pipeline.PromptTargetSystemPrefix) || target == string(pipeline.PromptTargetConversationPrefix)) {
				plan.SystemBlocks = append(plan.SystemBlocks, block)
				continue
			}
			plan.MessageBlocks = append(plan.MessageBlocks, block)
		}
	}

	if promptCacheEnabled(rc) && len(messages) > 0 {
		lastIndex := len(messages) - 1
		plan.MessageCache = llm.MessageCachePlan{
			Enabled:                   true,
			MarkerMessageIndex:        lastIndex,
			ToolResultCacheCutIndex:   lastIndex,
			ToolResultCacheReferences: true,
		}
	}

	if len(plan.SystemBlocks) == 0 && !plan.MessageCache.Enabled {
		return nil
	}
	return plan
}

func normalizePromptStability(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(pipeline.PromptStabilityVolatileTail):
		return llm.CacheStabilityVolatileTail
	case string(pipeline.PromptStabilitySessionPrefix):
		return llm.CacheStabilitySessionPrefix
	default:
		return llm.CacheStabilityStablePrefix
	}
}

func annotateToolCacheHints(rc *pipeline.RunContext, specs []llm.ToolSpec) []llm.ToolSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]llm.ToolSpec, len(specs))
	coreTools := map[string]struct{}{}
	if rc != nil && rc.PersonaDefinition != nil {
		for _, name := range rc.PersonaDefinition.CoreTools {
			coreTools[strings.TrimSpace(name)] = struct{}{}
		}
	}
	lastCacheableIndex := -1
	for i, spec := range specs {
		if spec.CacheHint != nil || !promptCacheEnabled(rc) {
			continue
		}
		lastCacheableIndex = i
	}
	for i, spec := range specs {
		out[i] = spec
		if spec.CacheHint != nil || !promptCacheEnabled(rc) {
			continue
		}
		if i != lastCacheableIndex {
			continue
		}
		scope := "global"
		if len(coreTools) > 0 {
			if _, ok := coreTools[strings.TrimSpace(spec.Name)]; !ok {
				scope = "org"
			}
		}
		out[i].CacheHint = &llm.CacheHint{
			Action: llm.CacheHintActionWrite,
			Scope:  scope,
		}
	}
	return out
}

func clonePromptPlan(src *llm.PromptPlan) *llm.PromptPlan {
	if src == nil {
		return nil
	}
	cloned := *src
	if len(src.SystemBlocks) > 0 {
		cloned.SystemBlocks = append([]llm.PromptPlanBlock(nil), src.SystemBlocks...)
	}
	if len(src.MessageBlocks) > 0 {
		cloned.MessageBlocks = append([]llm.PromptPlanBlock(nil), src.MessageBlocks...)
	}
	if len(src.MessageCache.PinnedCacheEdits) > 0 {
		cloned.MessageCache.PinnedCacheEdits = append([]llm.PromptCacheEditsBlock(nil), src.MessageCache.PinnedCacheEdits...)
	}
	if src.MessageCache.NewCacheEdits != nil {
		block := *src.MessageCache.NewCacheEdits
		if len(block.Edits) > 0 {
			block.Edits = append([]llm.PromptCacheEdit(nil), block.Edits...)
		}
		cloned.MessageCache.NewCacheEdits = &block
	}
	return &cloned
}

func cloneIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func cloneFloatPtr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func cloneToolChoice(src *llm.ToolChoice) *llm.ToolChoice {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}
