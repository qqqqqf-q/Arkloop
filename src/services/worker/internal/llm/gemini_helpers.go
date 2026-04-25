package llm

import (
	"fmt"
	"strings"

	"google.golang.org/genai"
)

func normalizeGeminiThinkingConfig(genConfig map[string]any, reasoningMode string) {
	rawThinkingConfig, hasThinkingConfig := genConfig["thinkingConfig"].(map[string]any)
	thinkingConfig := map[string]any{}
	if hasThinkingConfig {
		for key, value := range rawThinkingConfig {
			thinkingConfig[key] = value
		}
	}

	switch reasoningMode {
	case "enabled":
		if anyToInt(thinkingConfig["thinkingBudget"]) <= 0 {
			thinkingConfig["thinkingBudget"] = defaultGeminiThinkingBudget
		}
		thinkingConfig["includeThoughts"] = true
		genConfig["thinkingConfig"] = thinkingConfig
	case "disabled":
		thinkingConfig["thinkingBudget"] = 0
		thinkingConfig["includeThoughts"] = false
		genConfig["thinkingConfig"] = thinkingConfig
	default:
		if !hasThinkingConfig {
			return
		}
		if _, has := thinkingConfig["includeThoughts"]; !has {
			thinkingConfig["includeThoughts"] = anyToInt(thinkingConfig["thinkingBudget"]) > 0
		}
		genConfig["thinkingConfig"] = thinkingConfig
	}
}

func geminiPromptFeedbackFailure(feedback *genai.GenerateContentResponsePromptFeedback) *GatewayError {
	if feedback == nil || strings.TrimSpace(string(feedback.BlockReason)) == "" {
		return nil
	}
	reason := strings.TrimSpace(string(feedback.BlockReason))
	message := strings.TrimSpace(feedback.BlockReasonMessage)
	if message == "" {
		message = fmt.Sprintf("Gemini prompt blocked: %s", reason)
	}
	return &GatewayError{
		ErrorClass: ErrorClassPolicyDenied,
		Message:    message,
		Details:    map[string]any{"block_reason": reason},
	}
}

func geminiFinishReasonFailure(finishReason string) *GatewayError {
	reason := strings.TrimSpace(finishReason)
	switch reason {
	case "", "STOP", "MAX_TOKENS":
		return nil
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "LANGUAGE", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return &GatewayError{
			ErrorClass: ErrorClassPolicyDenied,
			Message:    fmt.Sprintf("Gemini content blocked: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS", "MALFORMED_RESPONSE", "MISSING_THOUGHT_SIGNATURE":
		return &GatewayError{
			ErrorClass: ErrorClassProviderNonRetryable,
			Message:    fmt.Sprintf("Gemini invalid response: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	default:
		return &GatewayError{
			ErrorClass: ErrorClassProviderRetryable,
			Message:    fmt.Sprintf("Gemini unexpected finish: %s", reason),
			Details:    map[string]any{"finish_reason": reason},
		}
	}
}
