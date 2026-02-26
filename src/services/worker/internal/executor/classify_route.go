package executor

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
)

// RouteConfig 描述单个分类分支的执行覆盖参数。
type RouteConfig struct {
	PromptOverride string
	ModelOverride  string // 空值表示沿用路由层已选 model
}

// ClassifyRouteExecutor 实现两阶段执行策略：
//  1. 轻量分类 LLM call — 内部步骤，不向 yield 透传事件
//  2. 按分类结果选定 prompt_override 做 single-shot 执行
type ClassifyRouteExecutor struct {
	classifyPrompt string
	defaultRoute   string
	routes         map[string]RouteConfig
}

// NewClassifyRouteExecutor 是 "task.classify_route" 的 Factory 函数。
func NewClassifyRouteExecutor(config map[string]any) (pipeline.AgentExecutor, error) {
	if config == nil {
		return nil, fmt.Errorf("executor_config is required for task.classify_route")
	}

	classifyPrompt, err := requiredString(config, "classify_prompt")
	if err != nil {
		return nil, err
	}

	defaultRoute, _ := config["default_route"].(string)
	defaultRoute = strings.TrimSpace(defaultRoute)

	routes, err := parseRoutes(config)
	if err != nil {
		return nil, err
	}

	if defaultRoute != "" {
		if _, ok := routes[defaultRoute]; !ok {
			return nil, fmt.Errorf("executor_config.default_route %q not found in routes", defaultRoute)
		}
	}

	return &ClassifyRouteExecutor{
		classifyPrompt: classifyPrompt,
		defaultRoute:   defaultRoute,
		routes:         routes,
	}, nil
}

func (e *ClassifyRouteExecutor) Execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	category, failed, err := e.classify(ctx, rc)
	if err != nil {
		return err
	}
	if failed != nil {
		errClass := failed.Error.ErrorClass
		return yield(emitter.Emit("run.failed", failed.ToDataJSON(), nil, &errClass))
	}

	routeCfg, ok := e.resolveRoute(category)
	if !ok {
		errPayload := llm.GatewayError{
			ErrorClass: "task.classify_route.no_match",
			Message:    fmt.Sprintf("no route for category %q", category),
		}
		return yield(emitter.Emit("run.failed", errPayload.ToJSON(), nil, &errPayload.ErrorClass))
	}

	return e.execute(ctx, rc, emitter, yield, routeCfg)
}

// classify 做内部分类 call，只收集文本输出；LLM 失败时返回 failed 事件数据。
func (e *ClassifyRouteExecutor) classify(ctx context.Context, rc *pipeline.RunContext) (string, *llm.StreamRunFailed, error) {
	req := llm.Request{
		Model: rc.SelectedRoute.Route.Model,
		Messages: append([]llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: e.classifyPrompt}}},
		}, rc.Messages...),
	}

	var chunks []string
	var failed *llm.StreamRunFailed
	sentinel := fmt.Errorf("stop")

	err := rc.Gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunFailed:
			failed = &typed
			return sentinel
		case llm.StreamRunCompleted:
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		return "", nil, err
	}
	if failed != nil {
		return "", failed, nil
	}
	return strings.TrimSpace(strings.Join(chunks, "")), nil, nil
}

// resolveRoute 按 category 查找 route，不命中时尝试 defaultRoute。
func (e *ClassifyRouteExecutor) resolveRoute(category string) (RouteConfig, bool) {
	if cfg, ok := e.routes[category]; ok {
		return cfg, true
	}
	if e.defaultRoute != "" {
		if cfg, ok := e.routes[e.defaultRoute]; ok {
			return cfg, true
		}
	}
	return RouteConfig{}, false
}

// execute 用选定的 prompt_override 做 single-shot LLM call，通过 yield 发出事件。
func (e *ClassifyRouteExecutor) execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
	routeCfg RouteConfig,
) error {
	model := rc.SelectedRoute.Route.Model
	if routeCfg.ModelOverride != "" {
		model = routeCfg.ModelOverride
	}

	req := llm.Request{
		Model: model,
		Messages: append([]llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: routeCfg.PromptOverride}}},
		}, rc.Messages...),
		MaxOutputTokens: rc.MaxOutputTokens,
	}

	var completedData map[string]any
	var failedData map[string]any
	var failedClass string
	sentinel := fmt.Errorf("stop")

	err := rc.Gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta != "" {
				return yield(emitter.Emit("message.delta", typed.ToDataJSON(), nil, nil))
			}
		case llm.StreamRunCompleted:
			completedData = typed.ToDataJSON()
			return sentinel
		case llm.StreamRunFailed:
			failedData = typed.ToDataJSON()
			failedClass = typed.Error.ErrorClass
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		return err
	}

	if failedData != nil {
		return yield(emitter.Emit("run.failed", failedData, nil, &failedClass))
	}
	if completedData != nil {
		return yield(emitter.Emit("run.completed", completedData, nil, nil))
	}

	// stream 结束但未收到 terminal 事件
	internal := llm.InternalStreamEndedError()
	return yield(emitter.Emit("run.failed", internal.ToJSON(), nil, &internal.ErrorClass))
}

// parseRoutes 从 executor_config 中解析 routes 字段。
func parseRoutes(config map[string]any) (map[string]RouteConfig, error) {
	raw, ok := config["routes"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("executor_config.routes is required")
	}
	routesMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("executor_config.routes must be an object")
	}
	if len(routesMap) == 0 {
		return nil, fmt.Errorf("executor_config.routes must not be empty")
	}

	routes := make(map[string]RouteConfig, len(routesMap))
	for key, val := range routesMap {
		rcMap, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("executor_config.routes[%q] must be an object", key)
		}
		promptOverride, err := requiredString(rcMap, "prompt_override")
		if err != nil {
			return nil, fmt.Errorf("executor_config.routes[%q].prompt_override: %w", key, err)
		}
		modelOverride, _ := rcMap["model_override"].(string)
		routes[key] = RouteConfig{
			PromptOverride: promptOverride,
			ModelOverride:  strings.TrimSpace(modelOverride),
		}
	}
	return routes, nil
}
