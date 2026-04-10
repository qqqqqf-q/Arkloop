package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/pgnotify"
	"github.com/jackc/pgx/v5"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/security"
)

const (
	userPromptSemanticModeDisabled            = "disabled"
	userPromptSemanticModeSync                = "sync"
	userPromptSemanticModeSpeculativeParallel = "speculative_parallel"
	injectionBlockedMessage                   = "message blocked: injection detected"
)

func formatInjectionBlockUserMessage(data map[string]any) string {
	if len(data) == 0 {
		return injectionBlockedMessage
	}
	parts := []string{"已拦截：检测到提示注入"}
	if raw, ok := data["patterns"]; ok {
		var lines []string
		appendPatternLine := func(m map[string]any) {
			if m == nil {
				return
			}
			cat, _ := m["category"].(string)
			pid, _ := m["pattern_id"].(string)
			sev, _ := m["severity"].(string)
			switch {
			case cat != "" && pid != "":
				lines = append(lines, cat+" / "+pid)
			case cat != "":
				lines = append(lines, cat)
			case pid != "":
				lines = append(lines, pid)
			case sev != "":
				lines = append(lines, sev)
			}
		}
		switch items := raw.(type) {
		case []map[string]any:
			for _, m := range items {
				appendPatternLine(m)
			}
		case []any:
			for _, it := range items {
				m, _ := it.(map[string]any)
				appendPatternLine(m)
			}
		}
		if len(lines) > 0 {
			parts = append(parts, "规则匹配："+strings.Join(lines, "，"))
		}
	}
	if sem, ok := data["semantic"].(map[string]any); ok {
		lbl, _ := sem["label"].(string)
		if sc, ok := floatFromAny(sem["score"]); ok && lbl != "" {
			parts = append(parts, fmt.Sprintf("语义判定：%s（%.2f）", lbl, sc))
		} else if lbl != "" {
			parts = append(parts, "语义判定："+lbl)
		}
	}
	if se, _ := data["semantic_error"].(string); strings.TrimSpace(se) != "" {
		parts = append(parts, "扫描降级："+strings.TrimSpace(se))
	}
	if len(parts) == 1 {
		return injectionBlockedMessage
	}
	return strings.Join(parts, "；")
}

func floatFromAny(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}

func injectionBlockedFailureDetails(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	out := map[string]any{}
	if v, ok := data["input_phase"]; ok {
		out["input_phase"] = v
	}
	if v, ok := data["detection_count"]; ok {
		out["detection_count"] = v
	}
	if v, ok := data["patterns"]; ok {
		out["patterns"] = v
	}
	if v, ok := data["semantic"]; ok {
		out["semantic"] = v
	}
	if v, ok := data["semantic_error"]; ok {
		out["semantic_error"] = v
	}
	if v, ok := data["injection"]; ok {
		out["injection"] = v
	}
	return out
}

func applyInjectionBlockUserFacingMessage(detectionData map[string]any) (userMsg string, blockedPayload map[string]any) {
	userMsg = formatInjectionBlockUserMessage(detectionData)
	blockedPayload = withBlockedMessage(detectionData)
	blockedPayload["message"] = userMsg
	return userMsg, blockedPayload
}

// NewInjectionScanMiddleware 在 Pipeline 中执行注入扫描。
// composite 为 nil 时整个 middleware 为 no-op。
func NewInjectionScanMiddleware(
	composite *security.CompositeScanner,
	auditor *security.SecurityAuditor,
	configResolver sharedconfig.Resolver,
	eventsRepo data.RunEventStore,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if composite == nil {
			security.ScanTotal.WithLabelValues("skipped").Inc()
			return next(ctx, rc)
		}

		regexEnabled := resolveEnabled(configResolver, "security.injection_scan.regex_enabled", true)
		semanticEnabled := resolveEnabled(configResolver, "security.injection_scan.semantic_enabled", false)
		blockingEnabled := resolveEnabled(configResolver, "security.injection_scan.blocking_enabled", false)
		toolScanEnabled := resolveEnabled(configResolver, "security.injection_scan.tool_output_scan_enabled", true)
		semanticProvider := resolveString(configResolver, "security.semantic_scanner.provider", "")
		userPromptSemanticMode := resolveUserPromptSemanticMode(semanticEnabled, semanticProvider)
		userPromptSemanticHotPath := userPromptSemanticMode == userPromptSemanticModeSync
		toolOutputSemanticEnabled := semanticEnabled && !strings.EqualFold(semanticProvider, "api")

		if !regexEnabled && !semanticEnabled {
			return next(ctx, rc)
		}

		if regexEnabled && len(rc.InjectionScanUserTexts) > 0 && composite != nil {
			maybeStripHistoricRegexInjectionFromMergedUser(ctx, rc, composite)
		}

		emitRunEvent(ctx, rc, eventsRepo, "security.scan.started", map[string]any{
			"input_phase":                   "initial",
			"regex_enabled":                 regexEnabled,
			"semantic_enabled":              semanticEnabled,
			"user_prompt_semantic_mode":     userPromptSemanticMode,
			"user_prompt_semantic_hot_path": userPromptSemanticHotPath,
			"semantic_provider":             semanticProvider,
			"tool_output_semantic_enabled":  toolOutputSemanticEnabled,
		})

		userTexts := injectionScanTextsForRun(rc, rc.Messages)
		regexMatches := scanUserPromptRegex(ctx, rc, composite, regexEnabled, userTexts)
		if len(regexMatches) > 0 {
			security.ScanTotal.WithLabelValues("detected").Inc()
			if auditor != nil {
				auditor.EmitInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, regexMatches, nil)
			}
			eventData := buildInjectionEventData(regexMatches, nil, "", "regex_match", true)
			eventData["input_phase"] = "initial"
			emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", eventData)

			if blockingEnabled {
				return blockRun(ctx, rc, eventsRepo, eventData)
			}
		} else if semanticEnabled && len(userTexts) > 0 {
			switch userPromptSemanticMode {
			case userPromptSemanticModeSpeculativeParallel:
				startSpeculativeUserPromptSemanticScan(ctx, rc, composite, auditor, eventsRepo, userTexts, blockingEnabled, "initial")
			case userPromptSemanticModeSync:
				semanticResult, semanticError, injectionDetected := scanUserPromptSemantic(ctx, rc, composite, userTexts)
				if injectionDetected {
					security.ScanTotal.WithLabelValues("detected").Inc()
					if auditor != nil {
						auditor.EmitInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, nil, semanticResult)
					}
					eventData := buildInjectionEventData(nil, semanticResult, semanticError, "", true)
					eventData["input_phase"] = "initial"
					emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", eventData)
					if blockingEnabled {
						return blockRun(ctx, rc, eventsRepo, eventData)
					}
				} else if semanticError != "" {
					security.ScanTotal.WithLabelValues("clean").Inc()
					emitRunEvent(ctx, rc, eventsRepo, "security.scan.degraded", map[string]any{
						"input_phase":    "initial",
						"semantic_error": semanticError,
					})
				} else {
					security.ScanTotal.WithLabelValues("clean").Inc()
					emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
						"input_phase": "initial",
					})
				}
			default:
				security.ScanTotal.WithLabelValues("clean").Inc()
				emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
					"input_phase": "initial",
				})
			}
		} else {
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
				"input_phase": "initial",
			})
		}

		rc.UserPromptScanFunc = buildUserPromptScanFunc(composite, auditor, configResolver, eventsRepo, rc)

		// 为 agent loop 注入 tool output 扫描函数
		if toolScanEnabled {
			rc.ToolOutputScanFunc = buildToolOutputScanFunc(composite, regexEnabled, toolOutputSemanticEnabled, auditor, eventsRepo, rc)
		}

		return next(ctx, rc)
	}
}

// blockRun 拦截注入请求：写入 blocked 事件 + run.failed，更新 run 状态。
func blockRun(ctx context.Context, rc *RunContext, eventsRepo data.RunEventStore, detectionData map[string]any) error {
	userMsg, blockedData := applyInjectionBlockUserFacingMessage(detectionData)
	emitRunEvent(ctx, rc, eventsRepo, "security.injection.blocked", blockedData)

	failedData := map[string]any{
		"error_class": "security.injection_blocked",
		"message":     userMsg,
	}
	if details := injectionBlockedFailureDetails(detectionData); len(details) > 0 {
		failedData["details"] = details
	}
	failedEvent := rc.Emitter.Emit("run.failed", failedData, nil, StringPtr("security.injection_blocked"))

	db := runEventDB(rc)
	if db == nil {
		return fmt.Errorf("injection block db unavailable")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("injection block tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, failedEvent); err != nil {
		return fmt.Errorf("injection block append event: %w", err)
	}

	runsRepo := rc.RunStatusDB
	if runsRepo == nil {
		return fmt.Errorf("injection block run status db unavailable")
	}
	if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, rc.Run.ID, data.TerminalStatusUpdate{
		Status: "failed",
	}); err != nil {
		return fmt.Errorf("injection block update status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("injection block commit: %w", err)
	}
	notifyRunEventSubscribers(ctx, rc)

	rc.ChannelTerminalNotice = userMsg
	TryDeliverChannelInjectionBlockNotice(ctx, rc.Pool, rc, userMsg)

	slog.WarnContext(ctx, "run blocked: injection detected", "run_id", rc.Run.ID)
	return nil
}

// buildToolOutputScanFunc 构建 tool output 扫描函数，用于 agent loop 中的间接注入检测。
func buildToolOutputScanFunc(
	composite *security.CompositeScanner,
	regexEnabled, semanticEnabled bool,
	auditor *security.SecurityAuditor,
	eventsRepo data.RunEventStore,
	rc *RunContext,
) func(string, string) (string, bool) {
	scanOptions := security.CompositeScanOptions{
		RegexEnabled:                regexEnabled,
		SemanticEnabled:             semanticEnabled,
		ShortCircuitOnDecisiveRegex: true,
	}

	return func(toolName, text string) (string, bool) {
		result := composite.Scan(context.Background(), text, scanOptions)

		detected := false
		var allDetections []security.ScanResult
		var semanticResult *security.SemanticResult
		var semanticError string

		if regexEnabled && len(result.RegexMatches) > 0 {
			detected = true
			allDetections = result.RegexMatches
			for _, r := range result.RegexMatches {
				security.DetectionTotal.WithLabelValues("tool_output_" + r.Category).Inc()
			}
		}
		if semanticEnabled && result.SemanticResult != nil {
			semanticResult = result.SemanticResult
			if result.SemanticResult.IsInjection {
				detected = true
				security.DetectionTotal.WithLabelValues("tool_output_semantic_" + strings.ToLower(result.SemanticResult.Label)).Inc()
			}
		}
		if result.SemanticError != "" {
			semanticError = result.SemanticError
		}

		// 闭包内无请求级 context，用 Background 做 best-effort 记录
		ctx := context.Background()

		if detected {
			slog.Warn("indirect injection detected in tool output",
				"run_id", rc.Run.ID,
				"tool_name", toolName,
				"regex_matches", len(allDetections),
			)
			security.ScanTotal.WithLabelValues("tool_output_detected").Inc()

			if auditor != nil {
				auditor.EmitToolOutputInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, toolName, allDetections, semanticResult)
			}

			eventData := map[string]any{
				"tool_name":       toolName,
				"detection_count": len(allDetections),
			}
			if result.SemanticSkipReason != "" {
				eventData["semantic_skipped"] = true
				eventData["semantic_skip_reason"] = result.SemanticSkipReason
			}
			if semanticError != "" {
				eventData["semantic_error"] = semanticError
			}
			if semanticResult != nil {
				eventData["semantic"] = map[string]any{
					"label": semanticResult.Label,
					"score": semanticResult.Score,
				}
			}
			emitRunEvent(ctx, rc, eventsRepo, "security.tool_output.detected", eventData)

			return "[content filtered: potential injection detected in tool output]", true
		}

		security.ScanTotal.WithLabelValues("tool_output_clean").Inc()
		if semanticError != "" {
			emitRunEvent(ctx, rc, eventsRepo, "security.tool_output.degraded", map[string]any{
				"tool_name":      toolName,
				"semantic_error": semanticError,
			})
		} else {
			emitRunEvent(ctx, rc, eventsRepo, "security.tool_output.clean", map[string]any{
				"tool_name": toolName,
			})
		}

		return "", false
	}
}

func emitRunEvent(ctx context.Context, rc *RunContext, eventsRepo data.RunEventStore, eventType string, dataJSON map[string]any) {
	ev := rc.Emitter.Emit(eventType, dataJSON, nil, nil)
	db := runEventDB(rc)
	if db == nil {
		slog.WarnContext(ctx, "injection scan event db unavailable")
		return
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.WarnContext(ctx, "injection scan event tx begin failed", "error", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); err != nil {
		slog.WarnContext(ctx, "injection scan event append failed", "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "injection scan event commit failed", "error", err)
		return
	}
	notifyRunEventSubscribers(ctx, rc)
}

func resolveUserPromptSemanticMode(semanticEnabled bool, provider string) string {
	if !semanticEnabled {
		return userPromptSemanticModeDisabled
	}
	if strings.EqualFold(strings.TrimSpace(provider), "api") {
		return userPromptSemanticModeSpeculativeParallel
	}
	return userPromptSemanticModeSync
}

func userMessageScanTextVariants(msg llm.Message) []string {
	if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
		return nil
	}
	partTexts := make([]string, 0, len(msg.Content))
	for _, part := range msg.Content {
		text := strings.TrimSpace(partPromptScanText(part))
		if text != "" {
			partTexts = append(partTexts, text)
		}
	}
	if len(partTexts) == 0 {
		return nil
	}
	texts := make([]string, 0, len(partTexts)+1)
	combined := strings.TrimSpace(strings.Join(partTexts, "\n\n"))
	if combined != "" {
		texts = append(texts, combined)
	}
	texts = append(texts, partTexts...)
	return uniqueTrimmedTexts(texts)
}

func collectUserPromptTexts(messages []llm.Message) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			continue
		}
		return userMessageScanTextVariants(messages[i])
	}
	return nil
}

func injectionScanTextsForRun(rc *RunContext, messages []llm.Message) []string {
	if rc != nil && len(rc.InjectionScanUserTexts) > 0 {
		return rc.InjectionScanUserTexts
	}
	return collectUserPromptTexts(messages)
}

func lastUserMessageIndex(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			return i
		}
	}
	return -1
}

func maybeStripHistoricRegexInjectionFromMergedUser(ctx context.Context, rc *RunContext, composite *security.CompositeScanner) {
	if rc == nil || len(rc.InjectionScanUserTexts) == 0 || composite == nil {
		return
	}
	idx := lastUserMessageIndex(rc.Messages)
	if idx < 0 {
		return
	}
	text, ok := singleTextMessage(rc.Messages[idx])
	if !ok || strings.TrimSpace(text) == "" {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(text), "---\n") {
		if newText, changed := stripHistoricRegexInjectionTelegramCompact(text, ctx, rc, composite); changed {
			rc.Messages[idx] = llm.Message{
				Role:    "user",
				Content: []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: newText}},
			}
		}
		return
	}
	if !strings.Contains(text, "\n\n") {
		return
	}
	paras := strings.Split(text, "\n\n")
	if len(paras) <= 1 {
		return
	}
	kept := make([]string, 0, len(paras))
	for i, p := range paras {
		if i == len(paras)-1 {
			kept = append(kept, p)
			continue
		}
		pt := strings.TrimSpace(p)
		if pt == "" {
			continue
		}
		if len(scanUserPromptRegex(ctx, rc, composite, true, []string{pt})) > 0 {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == len(paras) {
		return
	}
	newText := strings.Join(kept, "\n\n")
	rc.Messages[idx] = llm.Message{
		Role:    "user",
		Content: []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: newText}},
	}
}

func splitTelegramPublicMetaAndBody(text string) (header, body string, ok bool) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", false
	}
	rest := strings.TrimPrefix(normalized, "---\n")
	parts := strings.SplitN(rest, "\n---\n", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func splitTelegramCompactBurstSegments(body string) []string {
	lines := strings.Split(body, "\n")
	var segments []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		segments = append(segments, strings.TrimRight(b.String(), "\n"))
		b.Reset()
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		isNewSpeaker := t != "" && strings.HasPrefix(t, "[") && strings.Contains(t, "] ") && !strings.HasPrefix(line, "  ")
		if isNewSpeaker {
			flush()
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	flush()
	return segments
}

func stripHistoricRegexInjectionTelegramCompact(text string, ctx context.Context, rc *RunContext, composite *security.CompositeScanner) (string, bool) {
	header, body, ok := splitTelegramPublicMetaAndBody(text)
	if !ok {
		return text, false
	}
	segments := splitTelegramCompactBurstSegments(body)
	if len(segments) < 2 {
		return text, false
	}
	kept := make([]string, 0, len(segments))
	for i, seg := range segments {
		if i == len(segments)-1 {
			kept = append(kept, seg)
			continue
		}
		check := strings.TrimSpace(seg)
		if check == "" {
			continue
		}
		if len(scanUserPromptRegex(ctx, rc, composite, true, []string{check})) > 0 {
			continue
		}
		kept = append(kept, seg)
	}
	if len(kept) == len(segments) {
		return text, false
	}
	newBody := strings.Join(kept, "\n")
	out := "---\n" + header + "\n---\n" + newBody
	return out, true
}

func scanUserPromptRegex(
	ctx context.Context,
	rc *RunContext,
	composite *security.CompositeScanner,
	regexEnabled bool,
	texts []string,
) []security.ScanResult {
	if !regexEnabled || composite == nil || len(texts) == 0 {
		return nil
	}

	allMatches := make([]security.ScanResult, 0)
	for _, text := range texts {
		result := composite.Scan(ctx, text, security.CompositeScanOptions{
			RegexEnabled:    true,
			SemanticEnabled: false,
		})
		for _, match := range result.RegexMatches {
			slog.WarnContext(ctx, "injection pattern detected",
				"run_id", rc.Run.ID,
				"pattern_id", match.PatternID,
				"category", match.Category,
				"severity", match.Severity,
			)
			security.DetectionTotal.WithLabelValues(match.Category).Inc()
			allMatches = append(allMatches, match)
		}
	}
	return dedupeScanResults(allMatches)
}

func scanUserPromptSemantic(
	ctx context.Context,
	rc *RunContext,
	composite *security.CompositeScanner,
	texts []string,
) (*security.SemanticResult, string, bool) {
	if composite == nil || len(texts) == 0 {
		return nil, "", false
	}

	var semanticResult *security.SemanticResult
	var semanticError string
	for _, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, "", false
		}
		result := composite.Scan(ctx, text, security.CompositeScanOptions{
			RegexEnabled:    false,
			SemanticEnabled: true,
		})
		if err := ctx.Err(); err != nil {
			return nil, "", false
		}
		if result.SemanticError != "" {
			semanticError = result.SemanticError
		}
		if result.SemanticResult != nil {
			semanticResult = result.SemanticResult
			if result.SemanticResult.IsInjection {
				slog.WarnContext(ctx, "semantic injection detected",
					"run_id", rc.Run.ID,
					"label", result.SemanticResult.Label,
					"score", result.SemanticResult.Score,
				)
				security.DetectionTotal.WithLabelValues("semantic_" + strings.ToLower(result.SemanticResult.Label)).Inc()
				return semanticResult, semanticError, true
			}
		}
	}
	return semanticResult, semanticError, false
}

func startSpeculativeUserPromptSemanticScan(
	ctx context.Context,
	rc *RunContext,
	composite *security.CompositeScanner,
	auditor *security.SecurityAuditor,
	eventsRepo data.RunEventStore,
	texts []string,
	blockingEnabled bool,
	phase string,
) {
	go func() {
		semanticResult, semanticError, injectionDetected := scanUserPromptSemantic(ctx, rc, composite, texts)
		if ctx.Err() != nil {
			return
		}

		if injectionDetected {
			security.ScanTotal.WithLabelValues("detected").Inc()
			if auditor != nil {
				auditor.EmitInjectionDetected(context.Background(), rc.Run.ID, rc.Run.AccountID, rc.UserID, nil, semanticResult)
			}
			eventData := buildInjectionEventData(nil, semanticResult, semanticError, "", true)
			if strings.TrimSpace(phase) != "" {
				eventData["input_phase"] = phase
			}
			emitRunEvent(context.Background(), rc, eventsRepo, "security.injection.detected", eventData)
			if !blockingEnabled {
				return
			}
			if err := cancelRunForSpeculativeInjectionBlock(context.Background(), rc, eventsRepo, eventData); err != nil {
				slog.Warn("speculative semantic block failed", "run_id", rc.Run.ID, "error", err)
			}
			return
		}

		security.ScanTotal.WithLabelValues("clean").Inc()
		if semanticError != "" {
			data := map[string]any{
				"semantic_error": semanticError,
			}
			if strings.TrimSpace(phase) != "" {
				data["input_phase"] = phase
			}
			emitRunEvent(context.Background(), rc, eventsRepo, "security.scan.degraded", data)
			return
		}
		data := map[string]any{}
		if strings.TrimSpace(phase) != "" {
			data["input_phase"] = phase
		}
		emitRunEvent(context.Background(), rc, eventsRepo, "security.scan.clean", data)
	}()
}

func buildInjectionEventData(
	regexMatches []security.ScanResult,
	semanticResult *security.SemanticResult,
	semanticError string,
	semanticSkipReason string,
	injectionDetected bool,
) map[string]any {
	patterns := make([]map[string]any, 0, len(regexMatches))
	for _, match := range regexMatches {
		patterns = append(patterns, map[string]any{
			"pattern_id": match.PatternID,
			"category":   match.Category,
			"severity":   match.Severity,
		})
	}

	eventData := map[string]any{
		"detection_count": len(regexMatches),
		"patterns":        patterns,
		"injection":       injectionDetected,
	}
	if semanticSkipReason != "" {
		eventData["semantic_skipped"] = true
		eventData["semantic_skip_reason"] = semanticSkipReason
	}
	if semanticError != "" {
		eventData["semantic_error"] = semanticError
	}
	if semanticResult != nil {
		eventData["semantic"] = map[string]any{
			"label": semanticResult.Label,
			"score": semanticResult.Score,
		}
	}
	return eventData
}

func withBlockedMessage(data map[string]any) map[string]any {
	next := map[string]any{}
	for key, value := range data {
		next[key] = value
	}
	if _, ok := next["message"]; !ok {
		next["message"] = injectionBlockedMessage
	}
	return next
}

func cancelRunForSpeculativeInjectionBlock(
	ctx context.Context,
	rc *RunContext,
	eventsRepo data.RunEventStore,
	detectionData map[string]any,
) error {
	userMsg, blockedPayload := applyInjectionBlockUserFacingMessage(detectionData)
	emitRunEvent(ctx, rc, eventsRepo, "security.injection.blocked", blockedPayload)

	db := runEventDB(rc)
	if db == nil {
		return fmt.Errorf("speculative injection cancel db unavailable")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("speculative injection cancel tx: %w", err)
	}
	defer tx.Rollback(ctx)

	runsRepo := rc.RunStatusDB
	if runsRepo == nil {
		return fmt.Errorf("speculative injection cancel run status db unavailable")
	}
	if err := runsRepo.LockRunRow(ctx, tx, rc.Run.ID); err != nil {
		return fmt.Errorf("speculative injection cancel lock run: %w", err)
	}

	terminalType, err := eventsRepo.GetLatestEventType(ctx, tx, rc.Run.ID, []string{"run.completed", "run.failed", "run.cancelled", "run.interrupted"})
	if err != nil {
		return fmt.Errorf("speculative injection cancel read terminal state: %w", err)
	}
	if terminalType != "" {
		return nil
	}

	existingCancelType, err := eventsRepo.GetLatestEventType(ctx, tx, rc.Run.ID, []string{"run.cancel_requested", "run.cancelled"})
	if err != nil {
		return fmt.Errorf("speculative injection cancel read cancel state: %w", err)
	}
	if existingCancelType == "run.cancelled" {
		return nil
	}

	if existingCancelType == "" {
		cancelRequested := rc.Emitter.Emit("run.cancel_requested", map[string]any{
			"trace_id": rc.TraceID,
			"reason":   "security_injection_blocked",
			"message":  userMsg,
		}, nil, nil)
		if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, cancelRequested); err != nil {
			return fmt.Errorf("speculative injection cancel append event: %w", err)
		}
	}

	if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, rc.Run.ID, data.TerminalStatusUpdate{
		Status: "cancelled",
	}); err != nil {
		return fmt.Errorf("speculative injection cancel update status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("speculative injection cancel commit: %w", err)
	}

	notifyRunEventSubscribers(ctx, rc)
	rc.ChannelTerminalNotice = userMsg
	if rc.Pool != nil {
		_, _ = rc.Pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunCancel, rc.Run.ID.String())
	}
	if rc.CancelFunc != nil {
		rc.CancelFunc()
	}
	return nil
}

func buildUserPromptScanFunc(
	composite *security.CompositeScanner,
	auditor *security.SecurityAuditor,
	configResolver sharedconfig.Resolver,
	eventsRepo data.RunEventStore,
	rc *RunContext,
) func(context.Context, string, string) error {
	if composite == nil {
		return nil
	}

	return func(ctx context.Context, text string, phase string) error {
		texts := uniqueTrimmedTexts([]string{text})
		if len(texts) == 0 {
			return nil
		}

		regexEnabled := resolveEnabled(configResolver, "security.injection_scan.regex_enabled", true)
		semanticEnabled := resolveEnabled(configResolver, "security.injection_scan.semantic_enabled", false)
		blockingEnabled := resolveEnabled(configResolver, "security.injection_scan.blocking_enabled", false)
		semanticProvider := resolveString(configResolver, "security.semantic_scanner.provider", "")
		userPromptSemanticMode := resolveUserPromptSemanticMode(semanticEnabled, semanticProvider)

		emitRunEvent(ctx, rc, eventsRepo, "security.scan.started", map[string]any{
			"input_phase":                   phase,
			"regex_enabled":                 regexEnabled,
			"semantic_enabled":              semanticEnabled,
			"user_prompt_semantic_mode":     userPromptSemanticMode,
			"user_prompt_semantic_hot_path": userPromptSemanticMode == userPromptSemanticModeSync,
			"semantic_provider":             semanticProvider,
		})

		regexMatches := scanUserPromptRegex(ctx, rc, composite, regexEnabled, texts)
		if len(regexMatches) > 0 {
			security.ScanTotal.WithLabelValues("detected").Inc()
			if auditor != nil {
				auditor.EmitInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, regexMatches, nil)
			}
			eventData := buildInjectionEventData(regexMatches, nil, "", "regex_match", true)
			eventData["input_phase"] = phase
			emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", eventData)
			if blockingEnabled {
				if err := cancelRunForSpeculativeInjectionBlock(context.Background(), rc, eventsRepo, eventData); err != nil {
					return err
				}
				return security.ErrInputBlocked
			}
			return nil
		}

		if !semanticEnabled {
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
				"input_phase": phase,
			})
			return nil
		}

		switch userPromptSemanticMode {
		case userPromptSemanticModeSpeculativeParallel:
			startSpeculativeUserPromptSemanticScan(ctx, rc, composite, auditor, eventsRepo, texts, blockingEnabled, phase)
			return nil
		case userPromptSemanticModeSync:
			semanticResult, semanticError, injectionDetected := scanUserPromptSemantic(ctx, rc, composite, texts)
			if injectionDetected {
				security.ScanTotal.WithLabelValues("detected").Inc()
				if auditor != nil {
					auditor.EmitInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, nil, semanticResult)
				}
				eventData := buildInjectionEventData(nil, semanticResult, semanticError, "", true)
				eventData["input_phase"] = phase
				emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", eventData)
				if blockingEnabled {
					if err := cancelRunForSpeculativeInjectionBlock(context.Background(), rc, eventsRepo, eventData); err != nil {
						return err
					}
					return security.ErrInputBlocked
				}
				return nil
			}
			if semanticError != "" {
				security.ScanTotal.WithLabelValues("clean").Inc()
				emitRunEvent(ctx, rc, eventsRepo, "security.scan.degraded", map[string]any{
					"input_phase":    phase,
					"semantic_error": semanticError,
				})
				return nil
			}
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
				"input_phase": phase,
			})
			return nil
		default:
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", map[string]any{
				"input_phase": phase,
			})
			return nil
		}
	}
}

func partPromptScanText(part llm.ContentPart) string {
	switch part.Kind() {
	case "image":
		if part.Attachment != nil {
			filename := strings.TrimSpace(part.Attachment.Filename)
			if filename != "" {
				return "image attachment " + filename
			}
		}
		return ""
	default:
		return llm.PartPromptText(part)
	}
}

func uniqueTrimmedTexts(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func dedupeScanResults(results []security.ScanResult) []security.ScanResult {
	if len(results) < 2 {
		return results
	}

	out := make([]security.ScanResult, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		key := result.PatternID + "\x00" + result.Category + "\x00" + result.Severity + "\x00" + result.MatchedText
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, result)
	}
	return out
}

func notifyRunEventSubscribers(ctx context.Context, rc *RunContext) {
	channel := fmt.Sprintf("run_events:%s", rc.Run.ID.String())
	if rc.Pool != nil {
		_, _ = rc.Pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)
	}
	if rc.EventBus != nil {
		_ = rc.EventBus.Publish(ctx, channel, "")
	}

	if rc.BroadcastRDB != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", rc.Run.ID.String())
		_, _ = rc.BroadcastRDB.Publish(ctx, redisChannel, "").Result()
	}
}

func runEventDB(rc *RunContext) data.DB {
	if rc == nil {
		return nil
	}
	if rc.DB != nil {
		return rc.DB
	}
	if rc.Pool != nil {
		return rc.Pool
	}
	return nil
}
