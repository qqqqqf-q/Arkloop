package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"

	"github.com/pkoukk/tiktoken-go"
)

const defaultChannelGroupMaxContextTokens = 16384

// 群聊截断在 Routing 之前执行，没有选定模型；用 o200k 统一估算正文，与 HistoryThreadPromptTokens 默认回退一致。
// 每条里的图片单独加固定预算（PartPromptText 对 image 为空，不能仅靠正文 tiktoken）。
const groupTrimVisionTokensPerImage = 1024

var (
	groupTrimEncOnce sync.Once
	groupTrimEnc     *tiktoken.Tiktoken
	groupTrimEncErr  error
)

func groupTrimEncoder() *tiktoken.Tiktoken {
	groupTrimEncOnce.Do(func() {
		groupTrimEnc, groupTrimEncErr = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	})
	if groupTrimEncErr != nil {
		return nil
	}
	return groupTrimEnc
}

// NewChannelGroupContextTrimMiddleware 在 Channel 群聊 Run 上按近似 token 预算裁剪 Thread 历史（保留时间轴尾部）。
func NewChannelGroupContextTrimMiddleware() RunMiddleware {
	maxTokens := defaultChannelGroupMaxContextTokens
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_MAX_CONTEXT_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxTokens = n
		}
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		_ = ctx
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}
		trimRunContextMessagesToApproxTokens(rc, maxTokens)
		return next(ctx, rc)
	}
}

// IsTelegramGroupLikeConversation 判断 Telegram 侧群 / 超级群 / 频道（非私信）。
func IsTelegramGroupLikeConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

func trimRunContextMessagesToApproxTokens(rc *RunContext, maxTokens int) {
	if rc == nil || maxTokens <= 0 || len(rc.Messages) == 0 {
		return
	}
	msgs := rc.Messages
	ids := rc.ThreadMessageIDs
	alignedIDs := len(ids) == len(msgs)

	total := 0
	start := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		t := messageTokens(&msgs[i])
		if total+t > maxTokens {
			if start == len(msgs) {
				start = i
			}
			break
		}
		total += t
		start = i
	}
	if start <= 0 || start >= len(msgs) {
		return
	}
	rc.Messages = msgs[start:]
	if alignedIDs {
		rc.ThreadMessageIDs = ids[start:]
	}
}

// messageTokens 估算单条在历史截断里的权重，顺序：
// 1) assistant 且 usage_records.output_tokens>0（模型侧真实 completion，Desktop 与 Postgres 的 ListByThread 均已 JOIN）
// 2) tiktoken o200k 估正文+tool；图按固定 vision 预算
// 3) tiktoken 初始化失败则 legacy：rune/3
//
// user 等角色不能用 output_tokens：metadata 里同 run_id 会 JOIN 到同一条 usage，数值语义不是「本条 user 长度」。
func messageTokens(m *llm.Message) int {
	if m != nil && strings.EqualFold(strings.TrimSpace(m.Role), "assistant") &&
		m.OutputTokens != nil && *m.OutputTokens > 0 {
		return int(*m.OutputTokens)
	}
	if m == nil {
		return 1
	}
	return approxLLMMessageTokens(*m)
}

func approxLLMMessageTokens(m llm.Message) int {
	enc := groupTrimEncoder()
	if enc == nil {
		return approxLLMMessageTokensLegacy(m)
	}
	const tokensPerMessage = 3
	n := tokensPerMessage
	n += len(enc.Encode(m.Role, nil, nil))
	body := messageText(m)
	for _, tc := range m.ToolCalls {
		body += "\n"
		body += tc.ToolName
		if b, err := json.Marshal(tc.ArgumentsJSON); err == nil {
			body += string(b)
		}
	}
	n += len(enc.Encode(body, nil, nil))
	for _, p := range m.Content {
		if p.Kind() == messagecontent.PartTypeImage {
			n += groupTrimVisionTokensPerImage
		}
	}
	if n < 1 {
		return 1
	}
	return n
}

func approxLLMMessageTokensLegacy(m llm.Message) int {
	n := 0
	for _, p := range m.Content {
		n += utf8.RuneCountInString(p.Text)
		n += utf8.RuneCountInString(p.ExtractedText)
		if p.Attachment != nil {
			n += 64
		}
		if len(p.Data) > 0 {
			raw := len(p.Data) / 4
			if p.Kind() == messagecontent.PartTypeImage && raw > 3072 {
				raw = 3072
			}
			n += raw
		}
	}
	for _, tc := range m.ToolCalls {
		n += utf8.RuneCountInString(tc.ToolName)
		if b, err := json.Marshal(tc.ArgumentsJSON); err == nil {
			n += len(b) / 4
		}
	}
	out := n / 3
	if out < 1 {
		return 1
	}
	return out
}
