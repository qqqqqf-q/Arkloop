package pipeline

import (
	"strings"

	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/pkoukk/tiktoken-go"
)

// ContextCompactSettings 来自平台配置，供 ContextCompactMiddleware 使用。
type ContextCompactSettings struct {
	Enabled bool

	// MaxMessages 尾部最多保留多少条消息；0 表示不按条数收缩。
	MaxMessages int

	MaxUserMessageTokens int
	MaxTotalTextTokens   int
	MaxUserTextBytes     int
	MaxTotalTextBytes    int

	PersistEnabled             bool
	PersistTriggerApproxTokens int
	// PersistTriggerContextPct 1–100：按「上下文窗口」比例计算触发阈值；0 表示仅用 PersistTriggerApproxTokens。
	PersistTriggerContextPct int
	// FallbackContextWindowTokens 路由无 available_catalog.context_length 时用于比例换算。
	FallbackContextWindowTokens int
	PersistKeepLastMessages       int
}

func approxTokensFromText(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func messageText(m llm.Message) string {
	var b strings.Builder
	for _, p := range m.Content {
		b.WriteString(llm.PartPromptText(p))
	}
	return b.String()
}

func countUserTokens(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			n += approxTokensFromText(messageText(msgs[i]))
		}
	}
	return n
}

func countTotalTokens(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		n += approxTokensFromText(messageText(msgs[i]))
	}
	return n
}

func countUserBytes(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			n += len(messageText(msgs[i]))
		}
	}
	return n
}

func countTotalBytes(msgs []llm.Message, start int) int {
	n := 0
	for i := start; i < len(msgs); i++ {
		n += len(messageText(msgs[i]))
	}
	return n
}

// stabilizeCompactStart 在「尾部条数上限」与「不以孤立 tool 开头」之间收敛切口。
func stabilizeCompactStart(msgs []llm.Message, start int, maxMessages int) int {
	if len(msgs) == 0 {
		return 0
	}
	maxIter := len(msgs) + 8
	for iter := 0; iter < maxIter; iter++ {
		for start > 0 && start < len(msgs) && msgs[start].Role == "tool" {
			start--
		}
		if maxMessages <= 0 || len(msgs)-start <= maxMessages {
			break
		}
		start++
		if start >= len(msgs) {
			start = len(msgs) - 1
			break
		}
	}
	for start < len(msgs)-1 && msgs[start].Role == "tool" {
		start++
	}
	return start
}

func budgetOK(msgs []llm.Message, start int, cfg ContextCompactSettings, enc *tiktoken.Tiktoken) bool {
	if cfg.MaxMessages > 0 && len(msgs)-start > cfg.MaxMessages {
		return false
	}
	if cfg.MaxUserMessageTokens > 0 && SuffixRoleAndContentTokens(enc, msgs, start, true) > cfg.MaxUserMessageTokens {
		return false
	}
	if cfg.MaxTotalTextTokens > 0 && SuffixRoleAndContentTokens(enc, msgs, start, false) > cfg.MaxTotalTextTokens {
		return false
	}
	if cfg.MaxUserTextBytes > 0 && countUserBytes(msgs, start) > cfg.MaxUserTextBytes {
		return false
	}
	if cfg.MaxTotalTextBytes > 0 && countTotalBytes(msgs, start) > cfg.MaxTotalTextBytes {
		return false
	}
	return true
}

// CompactThreadMessages 从头部裁掉消息直到满足预算；保证切口不以孤立的 tool 开头（尽力左扩）。
// enc 为 nil 时 token 类预算退化为字节/4 近似（仅供测试）；生产路径应传入非 nil。
// ids 若与 msgs 等长则同步裁切；否则 ids 原样截断或置 nil。
func CompactThreadMessages(msgs []llm.Message, ids []uuid.UUID, cfg ContextCompactSettings, enc *tiktoken.Tiktoken) ([]llm.Message, []uuid.UUID, int) {
	if len(msgs) == 0 {
		return msgs, ids, 0
	}
	start := 0
	if cfg.MaxMessages > 0 && len(msgs) > cfg.MaxMessages {
		start = len(msgs) - cfg.MaxMessages
	}
	start = stabilizeCompactStart(msgs, start, cfg.MaxMessages)
	for start < len(msgs) && !budgetOK(msgs, start, cfg, enc) {
		start++
		start = stabilizeCompactStart(msgs, start, cfg.MaxMessages)
	}
	if start <= 0 {
		return msgs, alignIDs(ids, len(msgs)), 0
	}
	out := make([]llm.Message, len(msgs)-start)
	copy(out, msgs[start:])
	var outIDs []uuid.UUID
	if len(ids) == len(msgs) {
		outIDs = make([]uuid.UUID, len(ids)-start)
		copy(outIDs, ids[start:])
	}
	return out, outIDs, start
}

func alignIDs(ids []uuid.UUID, n int) []uuid.UUID {
	if len(ids) == n {
		return ids
	}
	return nil
}

// ContextCompactHasActiveBudget enabled 为真且至少有一项预算大于 0。
func ContextCompactHasActiveBudget(cfg ContextCompactSettings) bool {
	if !cfg.Enabled {
		return false
	}
	return cfg.MaxMessages > 0 ||
		cfg.MaxUserMessageTokens > 0 ||
		cfg.MaxTotalTextTokens > 0 ||
		cfg.MaxUserTextBytes > 0 ||
		cfg.MaxTotalTextBytes > 0
}
