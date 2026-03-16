package personas

import (
	"fmt"
	"sort"
	"time"

	"arkloop/services/worker/internal/tools"
)

type Budgets struct {
	ReasoningIterations    *int
	ToolContinuationBudget *int
	MaxOutputTokens        *int
	ToolTimeoutMs          *int
	ToolBudget             map[string]any
	PerToolSoftLimits      tools.PerToolSoftLimits
	Temperature            *float64
	TopP                   *float64
}

type StringOverride struct {
	Set   bool
	Value string
}

type OptionalStringOverride struct {
	Set   bool
	Value *string
}

type EnumStringOverride struct {
	Set   bool
	Value string
}

type BudgetsOverride struct {
	HasReasoningIterations    bool
	ReasoningIterations       *int
	HasToolContinuationBudget bool
	ToolContinuationBudget    *int
	HasMaxOutputTokens        bool
	MaxOutputTokens           *int
	HasToolTimeoutMs          bool
	ToolTimeoutMs             *int
	HasToolBudget             bool
	ToolBudget                map[string]any
	HasPerToolSoftLimits      bool
	PerToolSoftLimits         tools.PerToolSoftLimits
	HasTemperature            bool
	Temperature               *float64
	HasTopP                   bool
	TopP                      *float64
}

type RoleOverride struct {
	SoulMD              StringOverride
	PromptMD            StringOverride
	HasToolAllowlist    bool
	ToolAllowlist       []string
	HasToolDenylist     bool
	ToolDenylist        []string
	Budgets             BudgetsOverride
	PreferredCredential OptionalStringOverride
	Model               OptionalStringOverride
	ReasoningMode       EnumStringOverride
	PromptCacheControl  EnumStringOverride
}

// TitleSummarizerConfig 控制 goroutine 模式下的标题自动生成。
type TitleSummarizerConfig struct {
	Prompt    string
	MaxTokens int
}

type Definition struct {
	ID                      string
	Version                 string
	Title                   string
	Description             *string
	UserSelectable          bool
	SelectorName            *string
	SelectorOrder           *int
	ToolAllowlist           []string
	ToolDenylist            []string
	CoreTools               []string // tools always visible in request.Tools; nil = all tools are core (backward compatible)
	Budgets                 Budgets
	SoulMD                  string
	PromptMD                string
	RoleSoulMD              string
	RolePromptMD            string
	ExecutorType            string         // 执行策略类型，默认 "agent.simple"
	ExecutorConfig          map[string]any // Executor 配置，默认 {}
	PreferredCredential     *string        // 偏好凭证名称，nil 表示不绑定
	Model                   *string        // model selector，优先 provider^model，其次兼容裸 model
	ReasoningMode           string
	PromptCacheControl      string
	Roles                   map[string]RoleOverride
	TitleSummarizer         *TitleSummarizerConfig // nil 表示此 persona 不自动生成标题
	UpdatedAt               time.Time              // DB persona 最后修改时间，用于版本漂移检测
	IsSystem                bool                   // 系统级 persona，不可被 DB 覆盖或删除
	IsBuiltin               bool                   // 内置 persona，随代码分发
	AllowPlatformDelegation bool                   // 允许 admin 用户调用 call_platform 委托平台管理操作
}

type Registry struct {
	byID map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{byID: map[string]Definition{}}
}

func (r *Registry) Register(def Definition) error {
	if def.ID == "" {
		return fmt.Errorf("persona.id must not be empty")
	}
	if _, exists := r.byID[def.ID]; exists {
		return fmt.Errorf("persona.id duplicate: %s", def.ID)
	}
	r.byID[def.ID] = def
	return nil
}

func (r *Registry) Get(personaID string) (Definition, bool) {
	def, ok := r.byID[personaID]
	return def, ok
}

// Set 覆盖写入，用于 DB persona 覆盖同 ID 的文件系统 persona。
func (r *Registry) Set(def Definition) {
	r.byID[def.ID] = def
}

func (r *Registry) ListIDs() []string {
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
