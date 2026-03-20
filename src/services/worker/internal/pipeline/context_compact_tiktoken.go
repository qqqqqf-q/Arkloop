package pipeline

import (
	"errors"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/pkoukk/tiktoken-go"
)

// ResolveTiktokenForRoute 按供应商/模型选择 tiktoken 编码；未知 OpenAI 兼容模型回退 o200k_base。
func ResolveTiktokenForRoute(sel *routing.SelectedProviderRoute) (*tiktoken.Tiktoken, error) {
	if sel == nil {
		return nil, errors.New("selected route is nil")
	}
	return ResolveTiktokenForProviderModel(sel.Credential.ProviderKind, sel.Route.Model)
}

// ResolveTiktokenForProviderModel OpenAI 系按模型名映射；Anthropic/Gemini 用与上游不完全一致但可重复的固定编码（见实现注释）。
func ResolveTiktokenForProviderModel(kind routing.ProviderKind, model string) (*tiktoken.Tiktoken, error) {
	model = strings.TrimSpace(model)
	switch kind {
	case routing.ProviderKindAnthropic:
		// Claude 非 BPE 兼容；历史上常用 cl100k 作上界估算，与 OpenAI 口径不同但优于字节/4。
		return tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	case routing.ProviderKindGemini:
		return tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	default:
		if model != "" {
			if enc, err := tiktoken.EncodingForModel(model); err == nil {
				return enc, nil
			}
		}
		return tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	}
}

// HistoryThreadPromptTokens 线程消息按 Chat Completions 常见格式估算消耗的 token（含消息帧），用于与「上下文窗口」占比对齐。
func HistoryThreadPromptTokens(enc *tiktoken.Tiktoken, msgs []llm.Message) int {
	if enc == nil || len(msgs) == 0 {
		return 0
	}
	const tokensPerMessage = 3
	n := 0
	for _, m := range msgs {
		n += tokensPerMessage
		n += len(enc.Encode(m.Role, nil, nil))
		n += len(enc.Encode(messageText(m), nil, nil))
	}
	n += 3
	return n
}

// SuffixRoleAndContentTokens 从 start 起按 role+正文累加 tiktoken，不设消息帧；与裁切预算（max_total_text_tokens 等）语义一致。
func SuffixRoleAndContentTokens(enc *tiktoken.Tiktoken, msgs []llm.Message, start int, userRoleOnly bool) int {
	if enc == nil {
		if userRoleOnly {
			return countUserTokens(msgs, start)
		}
		return countTotalTokens(msgs, start)
	}
	n := 0
	for i := start; i < len(msgs); i++ {
		if userRoleOnly && msgs[i].Role != "user" {
			continue
		}
		n += len(enc.Encode(msgs[i].Role, nil, nil))
		n += len(enc.Encode(messageText(msgs[i]), nil, nil))
	}
	return n
}
