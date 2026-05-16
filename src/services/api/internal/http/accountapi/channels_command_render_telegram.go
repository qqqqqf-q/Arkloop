package accountapi

import (
	"arkloop/services/shared/telegrambot"
	"fmt"
	"strings"
)

// Telegram callback_data 紧凑编码前缀。
// 全部设计在 64 bytes 以内（Telegram 硬限制）。
const (
	cbModelProviders   = "mp"       // 显示 provider 列表（也用于返回按钮）
	cbModelProviderFmt = "mp_%d"    // 显示 provider[i] 的模型
	cbModelPageFmt     = "mp_%d_%d" // provider[i] 第 pg 页
	cbModelSelectFmt   = "ms_%d"    // 选择 flat index 处的模型
	cbThinkFmt         = "thk_%s"   // 设置思考模式
	cbPersonaFmt       = "prs_%d"   // 选择 persona[i]
	cbDismiss          = "dismiss"  // 关闭键盘
)

const telegramModelsPerPage = 8

// BuildTelegramProvidersKeyboard 构建第一级 provider 列表键盘。
// ● 标记当前选中模型所在的 provider。
func BuildTelegramProvidersKeyboard(data ModelPickerData) *telegrambot.InlineKeyboardMarkup {
	var rows [][]telegrambot.InlineKeyboardButton
	currentRow := make([]telegrambot.InlineKeyboardButton, 0, 2)

	for i, pg := range data.Providers {
		label := pg.Name
		if hasSelectedModel(pg.Models) {
			label += " ●"
		}
		btn := telegrambot.InlineKeyboardButton{
			Text:         label,
			CallbackData: fmt.Sprintf(cbModelProviderFmt, i),
		}
		currentRow = append(currentRow, btn)
		if len(currentRow) == 2 {
			rows = append(rows, currentRow)
			currentRow = make([]telegrambot.InlineKeyboardButton, 0, 2)
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}
	rows = append(rows, []telegrambot.InlineKeyboardButton{{Text: "✕", CallbackData: cbDismiss}})
	return &telegrambot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// BuildTelegramProviderModelsKeyboard 构建第二级模型列表键盘（带分页）。
func BuildTelegramProviderModelsKeyboard(data ModelPickerData, providerIdx int, page int) *telegrambot.InlineKeyboardMarkup {
	if providerIdx < 0 || providerIdx >= len(data.Providers) {
		return nil
	}
	pg := data.Providers[providerIdx]
	total := len(pg.Models)
	totalPages := max(1, (total+telegramModelsPerPage-1)/telegramModelsPerPage)
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * telegramModelsPerPage
	end := min(total, start+telegramModelsPerPage)

	var rows [][]telegrambot.InlineKeyboardButton
	for i := start; i < end; i++ {
		m := pg.Models[i]
		// flat index = 所有 provider 中该模型的全局位置
		flatIdx := flattenModelIndex(data, providerIdx, i)
		label := m.DisplayName
		if m.IsSelected {
			label += " ✓"
		}
		rows = append(rows, []telegrambot.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf(cbModelSelectFmt, flatIdx),
		}})
	}

	// 分页导航 + 返回按钮
	navRow := make([]telegrambot.InlineKeyboardButton, 0, 3)
	if page > 0 {
		navRow = append(navRow, telegrambot.InlineKeyboardButton{
			Text:         "← 上一页",
			CallbackData: fmt.Sprintf(cbModelPageFmt, providerIdx, page-1),
		})
	}
	if totalPages > 1 {
		navRow = append(navRow, telegrambot.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d/%d", page+1, totalPages),
			CallbackData: cbDismiss,
		})
	}
	if page < totalPages-1 {
		navRow = append(navRow, telegrambot.InlineKeyboardButton{
			Text:         "下一页 →",
			CallbackData: fmt.Sprintf(cbModelPageFmt, providerIdx, page+1),
		})
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	rows = append(rows, []telegrambot.InlineKeyboardButton{
		{Text: "← 返回", CallbackData: cbModelProviders},
		{Text: "✕", CallbackData: cbDismiss},
	})

	return &telegrambot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// BuildTelegramModelQuickKeyboard 构建 /model 无参数时的快速切换键盘。
// 显示当前 provider 的模型 + "查看全部 →" 按钮。
func BuildTelegramModelQuickKeyboard(data ModelPickerData) *telegrambot.InlineKeyboardMarkup {
	currentProviderIdx := findCurrentProviderIndex(data)
	var rows [][]telegrambot.InlineKeyboardButton

	if currentProviderIdx >= 0 {
		pg := data.Providers[currentProviderIdx]
		limit := min(len(pg.Models), telegramModelsPerPage)
		for i := 0; i < limit; i++ {
			m := pg.Models[i]
			flatIdx := flattenModelIndex(data, currentProviderIdx, i)
			label := m.DisplayName
			if m.IsSelected {
				label += " ✓"
			}
			rows = append(rows, []telegrambot.InlineKeyboardButton{{
				Text:         label,
				CallbackData: fmt.Sprintf(cbModelSelectFmt, flatIdx),
			}})
		}
	}

	rows = append(rows, []telegrambot.InlineKeyboardButton{
		{Text: "查看全部 →", CallbackData: cbModelProviders},
		{Text: "✕", CallbackData: cbDismiss},
	})

	return &telegrambot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// BuildTelegramThinkKeyboard 构建 /think 键盘。
func BuildTelegramThinkKeyboard(data ThinkPickerData) *telegrambot.InlineKeyboardMarkup {
	var rows [][]telegrambot.InlineKeyboardButton
	for _, m := range data.Modes {
		label := m.Name
		if m.IsSelected {
			label += " ✓"
		}
		rows = append(rows, []telegrambot.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf(cbThinkFmt, m.Name),
		}})
	}
	rows = append(rows, []telegrambot.InlineKeyboardButton{{Text: "✕", CallbackData: cbDismiss}})
	return &telegrambot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// BuildTelegramPersonaKeyboard 构建 /persona 键盘。
func BuildTelegramPersonaKeyboard(data PersonaPickerData) *telegrambot.InlineKeyboardMarkup {
	var rows [][]telegrambot.InlineKeyboardButton
	for i, p := range data.Personas {
		label := p.DisplayName
		if p.IsSelected {
			label += " ✓"
		}
		rows = append(rows, []telegrambot.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf(cbPersonaFmt, i),
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	rows = append(rows, []telegrambot.InlineKeyboardButton{{Text: "✕", CallbackData: cbDismiss}})
	return &telegrambot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// BuildTelegramInteractive 从 CommandReply 的 Interactive 数据构建 Telegram 键盘。
func BuildTelegramInteractive(reply *CommandReply) *telegrambot.InlineKeyboardMarkup {
	if reply == nil || reply.Interactive == nil {
		return nil
	}
	switch d := reply.Interactive.(type) {
	case ModelPickerData:
		if d.ShowQuickSwitch {
			return BuildTelegramModelQuickKeyboard(d)
		}
		return BuildTelegramProvidersKeyboard(d)
	case ThinkPickerData:
		return BuildTelegramThinkKeyboard(d)
	case PersonaPickerData:
		return BuildTelegramPersonaKeyboard(d)
	default:
		return nil
	}
}

// FlattenModelChoices 返回所有 provider 下 ModelChoice 的平铺列表。
func FlattenModelChoices(data ModelPickerData) []ModelChoice {
	flat := make([]ModelChoice, 0)
	for _, pg := range data.Providers {
		flat = append(flat, pg.Models...)
	}
	return flat
}

// --- 内部辅助 ---

func hasSelectedModel(models []ModelChoice) bool {
	for _, m := range models {
		if m.IsSelected {
			return true
		}
	}
	return false
}

func findCurrentProviderIndex(data ModelPickerData) int {
	for i, pg := range data.Providers {
		if hasSelectedModel(pg.Models) {
			return i
		}
	}
	if len(data.Providers) > 0 {
		return 0
	}
	return -1
}

// flattenModelIndex 返回 model 在所有 provider models 平铺后的全局索引。
func flattenModelIndex(data ModelPickerData, providerIdx, modelIdx int) int {
	idx := 0
	for i := 0; i < providerIdx; i++ {
		idx += len(data.Providers[i].Models)
	}
	return idx + modelIdx
}

// ParseCallbackData 解析 Telegram callback_data，返回结构化结果。
type ParsedCallback struct {
	Kind        string // "providers", "provider_models", "model_select", "think", "persona", "dismiss", "back"
	ProviderIdx int    // mp_ / mp_{i}_{pg}
	Page        int    // mp_{i}_{pg}
	FlatIndex   int    // ms_{idx}
	ThinkMode   string // thk_{mode}
	PersonaIdx  int    // prs_{i}
}

func ParseCallbackData(data string) (ParsedCallback, bool) {
	if data == cbDismiss {
		return ParsedCallback{Kind: "dismiss"}, true
	}
	if data == cbModelProviders {
		return ParsedCallback{Kind: "providers"}, true
	}
	if strings.HasPrefix(data, "mp_") {
		rest := data[3:]
		parts := strings.SplitN(rest, "_", 2)
		providerIdx := 0
		if _, err := fmt.Sscanf(parts[0], "%d", &providerIdx); err != nil {
			return ParsedCallback{}, false
		}
		page := 0
		if len(parts) > 1 {
			if _, err := fmt.Sscanf(parts[1], "%d", &page); err != nil {
				return ParsedCallback{}, false
			}
		}
		if page > 0 {
			return ParsedCallback{Kind: "provider_models", ProviderIdx: providerIdx, Page: page}, true
		}
		return ParsedCallback{Kind: "provider_models", ProviderIdx: providerIdx, Page: 0}, true
	}
	if strings.HasPrefix(data, "ms_") {
		idx := 0
		if _, err := fmt.Sscanf(data[3:], "%d", &idx); err != nil {
			return ParsedCallback{}, false
		}
		return ParsedCallback{Kind: "model_select", FlatIndex: idx}, true
	}
	if strings.HasPrefix(data, "thk_") {
		mode := data[4:]
		validModes := map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "max": true}
		if !validModes[mode] {
			return ParsedCallback{}, false
		}
		return ParsedCallback{Kind: "think", ThinkMode: mode}, true
	}
	if strings.HasPrefix(data, "prs_") {
		idx := 0
		if _, err := fmt.Sscanf(data[4:], "%d", &idx); err != nil {
			return ParsedCallback{}, false
		}
		return ParsedCallback{Kind: "persona", PersonaIdx: idx}, true
	}
	return ParsedCallback{}, false
}
