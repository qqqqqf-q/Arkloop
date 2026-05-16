package accountapi

import (
	"arkloop/services/api/internal/data"
	"strings"

	"github.com/google/uuid"
)

// CommandReply 是命令执行的渠道无关结果。
// Text 始终包含完整的、可读的文本响应（纯文本渠道直接使用）。
// Interactive 携带可选的结构化数据，供有交互 UI 的渠道渲染。
type CommandReply struct {
	Text        string
	Interactive InteractiveData
	CancelRunID uuid.UUID
}

// InteractiveData 是密封接口，只有 ModelPickerData/ThinkPickerData/PersonaPickerData 满足。
type InteractiveData interface{ interactiveData() }

// --- 模型选择器 ---

// ModelPickerData 携带 /models 和 /model 的结构化数据。
// 支持两级导航：providers → 该 provider 下的 models。
type ModelPickerData struct {
	CurrentSelector string          // 当前 selector（如 "openai^gpt-4o"），空=跟随频道默认
	CurrentDisplay  string          // 当前模型的人类可读名
	Providers       []ProviderGroup // 按 credential 分组的模型
	AllowUserScoped bool
	ShowQuickSwitch bool // /model 无参数时为 true，显示快速切换键盘而非全量 provider 列表
}

func (ModelPickerData) interactiveData() {}

// ProviderGroup 表示一个 credential（provider）及其下的模型。
type ProviderGroup struct {
	Name   string // credential name，如 "OpenAI"
	Models []ModelChoice
}

// ModelChoice 表示一个可选的模型。
type ModelChoice struct {
	Selector    string // 完整 selector（credentialName^model）
	DisplayName string // 人类可读名（model 部分）
	IsSelected  bool
}

// --- 思考强度 ---

// ThinkPickerData 携带 /think 的结构化数据。
type ThinkPickerData struct {
	CurrentMode string
	Modes       []ThinkModeOption
}

func (ThinkPickerData) interactiveData() {}

// ThinkModeOption 表示一个思考强度选项。
type ThinkModeOption struct {
	Name       string
	IsSelected bool
}

// --- Persona ---

// PersonaPickerData 携带 /persona 的结构化数据。
type PersonaPickerData struct {
	CurrentName string
	Personas    []PersonaChoice
}

func (PersonaPickerData) interactiveData() {}

// PersonaChoice 表示一个可选的 persona。
type PersonaChoice struct {
	ID          string
	DisplayName string
	IsSelected  bool
}

// GroupCandidatesByProvider 将扁平候选列表转换为按 provider 分组的 ModelPickerData。
func GroupCandidatesByProvider(
	candidates []telegramSelectorCandidate,
	preferredSelector string,
	allowUserScoped bool,
) ModelPickerData {
	type group struct {
		name   string
		models []ModelChoice
	}
	groupMap := make(map[string]*group)
	var groupOrder []string

	for _, c := range candidates {
		if !c.accountScoped && !allowUserScoped {
			continue
		}
		selector := c.credentialName + "^" + c.model
		isSelected := strings.EqualFold(selector, strings.TrimSpace(preferredSelector)) ||
			strings.EqualFold(c.model, strings.TrimSpace(preferredSelector))

		g, exists := groupMap[c.credentialName]
		if !exists {
			g = &group{name: c.credentialName}
			groupMap[c.credentialName] = g
			groupOrder = append(groupOrder, c.credentialName)
		}
		g.models = append(g.models, ModelChoice{
			Selector:    selector,
			DisplayName: c.model,
			IsSelected:  isSelected,
		})
	}

	currentDisplay := preferredSelector
	if strings.TrimSpace(preferredSelector) == "" {
		currentDisplay = "跟随频道默认"
	}

	var providers []ProviderGroup
	for _, name := range groupOrder {
		providers = append(providers, ProviderGroup{
			Name:   name,
			Models: groupMap[name].models,
		})
	}

	return ModelPickerData{
		CurrentSelector: preferredSelector,
		CurrentDisplay:  currentDisplay,
		Providers:       providers,
		AllowUserScoped: allowUserScoped,
	}
}

// BuildPersonaPickerData 从 persona 列表构建 PersonaPickerData。
func BuildPersonaPickerData(
	personas []data.Persona,
	currentPersonaID uuid.UUID,
	currentDisplayName string,
) PersonaPickerData {
	var choices []PersonaChoice
	for _, p := range personas {
		if !p.UserSelectable {
			continue
		}
		choices = append(choices, PersonaChoice{
			ID:          p.ID.String(),
			DisplayName: p.DisplayName,
			IsSelected:  p.ID == currentPersonaID,
		})
	}
	return PersonaPickerData{
		CurrentName: currentDisplayName,
		Personas:    choices,
	}
}
