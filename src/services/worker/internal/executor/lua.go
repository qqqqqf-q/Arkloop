package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"

	lua "github.com/yuin/gopher-lua"
)

// LuaExecutor 实现 AgentExecutor 接口，通过内嵌 Lua 脚本描述编排逻辑。
// 每个 Execute 调用创建独立 LState，无共享状态，无需加锁。
type LuaExecutor struct {
	script string
}

// NewLuaExecutor 是 "agent.lua" 的工厂函数。
// executor_config 必须包含非空 script 字段。
func NewLuaExecutor(config map[string]any) (pipeline.AgentExecutor, error) {
	script, err := requiredString(config, "script")
	if err != nil {
		return nil, fmt.Errorf("executor_config.script: %w", err)
	}
	return &LuaExecutor{script: script}, nil
}

func (e *LuaExecutor) Execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()

	// 仅加载安全标准库，禁止 os/io/debug/package/channel
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// base 库包含 dofile/loadfile，可从文件系统加载代码，必须移除
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)

	rt := &luaRuntime{
		ctx:     ctx,
		rc:      rc,
		emitter: emitter,
		yield:   yield,
	}
	rt.register(L)

	if err := L.DoString(e.script); err != nil {
		errClass := "agent.lua.script_error"
		return yield(emitter.Emit("run.failed", map[string]any{
			"error_class": errClass,
			"message":     err.Error(),
		}, nil, &errClass))
	}

	// agent.loop() 已处理终态事件，无需重复发送
	if rt.loopTerminal {
		return nil
	}

	// 脚本执行完成，emit 最终输出
	output := strings.TrimSpace(rt.output)
	if output != "" {
		delta := llm.StreamMessageDelta{ContentDelta: output, Role: "assistant"}
		if err := yield(emitter.Emit("message.delta", delta.ToDataJSON(), nil, nil)); err != nil {
			return err
		}
	}

	completedData := map[string]any{}
	if usageJSON := rt.accumulatedUsage.ToJSON(); len(usageJSON) > 0 {
		completedData["usage"] = usageJSON
	}
	return yield(emitter.Emit("run.completed", completedData, nil, nil))
}

// luaRuntime 持有单次 Execute 调用的运行时状态，注册为 Lua bindings。
type luaRuntime struct {
	ctx     context.Context
	rc      *pipeline.RunContext
	emitter events.Emitter
	yield   func(events.RunEvent) error
	output  string
	// 累积 agent.generate / agent.stream 消耗的 token，随最终 run.completed 上报
	accumulatedUsage llm.Usage
	// agent.loop() 内部循环已发送终态事件，外层 Execute 不再重复发送
	loopTerminal bool
}

// mergeUsage 将一次 LLM 调用的 usage 累加到 accumulatedUsage。
func (rt *luaRuntime) mergeUsage(u *llm.Usage) {
	if u == nil {
		return
	}
	addInt := func(dst **int, src *int) {
		if src == nil {
			return
		}
		if *dst == nil {
			v := *src
			*dst = &v
		} else {
			**dst += *src
		}
	}
	addInt(&rt.accumulatedUsage.InputTokens, u.InputTokens)
	addInt(&rt.accumulatedUsage.OutputTokens, u.OutputTokens)
	addInt(&rt.accumulatedUsage.TotalTokens, u.TotalTokens)
	addInt(&rt.accumulatedUsage.CacheCreationInputTokens, u.CacheCreationInputTokens)
	addInt(&rt.accumulatedUsage.CacheReadInputTokens, u.CacheReadInputTokens)
	addInt(&rt.accumulatedUsage.CachedTokens, u.CachedTokens)
}

func (rt *luaRuntime) register(L *lua.LState) {
	agentTable := L.NewTable()
	L.SetField(agentTable, "spawn", L.NewFunction(rt.agentSpawn))
	L.SetField(agentTable, "send", L.NewFunction(rt.agentSend))
	L.SetField(agentTable, "wait", L.NewFunction(rt.agentWait))
	L.SetField(agentTable, "resume", L.NewFunction(rt.agentResume))
	L.SetField(agentTable, "close", L.NewFunction(rt.agentClose))
	L.SetField(agentTable, "classify", L.NewFunction(rt.agentClassify))
	L.SetField(agentTable, "generate", L.NewFunction(rt.agentGenerate))
	L.SetField(agentTable, "stream", L.NewFunction(rt.agentStream))
	L.SetField(agentTable, "stream_route", L.NewFunction(rt.agentStreamRoute))
	L.SetField(agentTable, "stream_agent", L.NewFunction(rt.agentStreamAgent))
	L.SetField(agentTable, "loop", L.NewFunction(rt.agentLoop))
	L.SetField(agentTable, "loop_capture", L.NewFunction(rt.agentLoopCapture))
	L.SetGlobal("agent", agentTable)

	toolsTable := L.NewTable()
	L.SetField(toolsTable, "call", L.NewFunction(rt.toolsCall))
	L.SetField(toolsTable, "call_parallel", L.NewFunction(rt.toolsCallParallel))
	L.SetGlobal("tools", toolsTable)

	contextTable := L.NewTable()
	L.SetField(contextTable, "get", L.NewFunction(rt.contextGet))
	L.SetField(contextTable, "set_output", L.NewFunction(rt.contextSetOutput))
	L.SetField(contextTable, "emit", L.NewFunction(rt.contextEmit))
	L.SetGlobal("context", contextTable)

	jsonTable := L.NewTable()
	L.SetField(jsonTable, "encode", L.NewFunction(jsonEncode))
	L.SetField(jsonTable, "decode", L.NewFunction(jsonDecode))
	L.SetGlobal("json", jsonTable)

	// memory binding：MemoryProvider 非 nil 时调用真实 provider，否则返回空/错误
	memoryTable := L.NewTable()
	L.SetField(memoryTable, "search", L.NewFunction(rt.memorySearch))
	L.SetField(memoryTable, "read", L.NewFunction(rt.memoryRead))
	L.SetField(memoryTable, "write", L.NewFunction(rt.memoryWrite))
	L.SetField(memoryTable, "forget", L.NewFunction(rt.memoryForget))
	L.SetGlobal("memory", memoryTable)
}

func (rt *luaRuntime) agentSpawn(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.SubAgentControl == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.spawn not available: SubAgentControl not initialized"))
		return 2
	}
	tbl, ok := L.Get(1).(*lua.LTable)
	if !ok {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.spawn: args must be a table"))
		return 2
	}
	req, err := rt.parseSpawnRequest(tbl)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	snapshot, err := rt.rc.SubAgentControl.Spawn(rt.ctx, req)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(statusSnapshotToLuaTable(L, snapshot))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) agentSend(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.SubAgentControl == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.send not available: SubAgentControl not initialized"))
		return 2
	}
	subAgentID, err := parseLuaSubAgentID(L.Get(1), "agent.send: id")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	input, err := luaRequiredString(L.Get(2), "agent.send: input")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	interrupt, err := parseLuaSendOptions(L.Get(3))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	snapshot, err := rt.rc.SubAgentControl.SendInput(rt.ctx, subagentctl.SendInputRequest{
		SubAgentID: subAgentID,
		Input:      input,
		Interrupt:  interrupt,
	})
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(statusSnapshotToLuaTable(L, snapshot))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) agentWait(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.SubAgentControl == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.wait not available: SubAgentControl not initialized"))
		return 2
	}
	subAgentID, err := parseLuaSubAgentID(L.Get(1), "agent.wait: id")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	timeout, err := parseLuaTimeout(L.Get(2), "agent.wait: timeout_ms")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	snapshot, err := rt.rc.SubAgentControl.Wait(rt.ctx, subagentctl.WaitRequest{SubAgentIDs: []uuid.UUID{subAgentID}, Timeout: timeout})
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(statusSnapshotToLuaTable(L, snapshot))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) agentResume(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.SubAgentControl == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.resume not available: SubAgentControl not initialized"))
		return 2
	}
	subAgentID, err := parseLuaSubAgentID(L.Get(1), "agent.resume: id")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	snapshot, err := rt.rc.SubAgentControl.Resume(rt.ctx, subagentctl.ResumeRequest{SubAgentID: subAgentID})
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(statusSnapshotToLuaTable(L, snapshot))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) agentClose(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.SubAgentControl == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.close not available: SubAgentControl not initialized"))
		return 2
	}
	subAgentID, err := parseLuaSubAgentID(L.Get(1), "agent.close: id")
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	snapshot, err := rt.rc.SubAgentControl.Close(rt.ctx, subagentctl.CloseRequest{SubAgentID: subAgentID})
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(statusSnapshotToLuaTable(L, snapshot))
	L.Push(lua.LNil)
	return 2
}

// agent.classify(prompt, labels) -> (label, err)
// labels 是 Lua table，如 {"label1", "label2"}。
// 轻量分类，不创建子 Run，直接调用 Gateway。
func (rt *luaRuntime) agentClassify(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.classify not available: gateway not initialized"))
		return 2
	}

	prompt := L.CheckString(1)
	labelsTable := L.CheckTable(2)

	var labels []string
	labelsTable.ForEach(func(_, v lua.LValue) {
		if s, ok := v.(lua.LString); ok {
			labels = append(labels, string(s))
		}
	})
	if len(labels) == 0 {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.classify: labels table must not be empty"))
		return 2
	}

	sysPrompt := fmt.Sprintf(
		"Classify into exactly one of: %s.\nRespond with only the label, nothing else.",
		strings.Join(labels, ", "),
	)
	req := llm.Request{
		Model: rt.rc.SelectedRoute.Route.Model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: sysPrompt}}},
			{Role: "user", Content: []llm.TextPart{{Text: prompt}}},
		},
	}

	var chunks []string
	var streamFailed *llm.StreamRunFailed
	sentinel := fmt.Errorf("stop")

	err := rt.rc.Gateway.Stream(rt.ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunFailed:
			streamFailed = &typed
			return sentinel
		case llm.StreamRunCompleted:
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	if streamFailed != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(streamFailed.Error.Message))
		return 2
	}

	L.Push(lua.LString(strings.TrimSpace(strings.Join(chunks, ""))))
	L.Push(lua.LNil)
	return 2
}

// tools.call(name, args_json) -> (result_json, err)
func (rt *luaRuntime) toolsCall(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.ToolExecutor == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("tools.call not available: tool executor not initialized"))
		return 2
	}

	toolName := L.CheckString(1)
	argsJSON := L.CheckString(2)

	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("tools.call: invalid args JSON: %s", err.Error())))
		return 2
	}

	execCtx := tools.ExecutionContext{
		RunID:                            rt.rc.Run.ID,
		TraceID:                          rt.rc.TraceID,
		AccountID:                        &rt.rc.Run.AccountID,
		ThreadID:                         &rt.rc.Run.ThreadID,
		ProjectID:                        rt.rc.Run.ProjectID,
		UserID:                           rt.rc.UserID,
		ProfileRef:                       rt.rc.ProfileRef,
		WorkspaceRef:                     rt.rc.WorkspaceRef,
		EnabledSkills:                    append([]skillstore.ResolvedSkill(nil), rt.rc.EnabledSkills...),
		ToolAllowlist:                    sortedToolNames(rt.rc.AllowlistSet),
		ToolDenylist:                     append([]string(nil), rt.rc.ToolDenylist...),
		ActiveToolProviderConfigsByGroup: copyProviderConfigMap(rt.rc.ActiveToolProviderConfigsByGroup),
		RouteID:                          routeIDFromRunContext(rt.rc),
		Model:                            modelFromRunContext(rt.rc),
		MemoryScope:                      "same_user",
		AgentID:                          agentIDFromPersona(rt.rc),
		TimeoutMs:                        rt.rc.ToolTimeoutMs,
		Budget:                           rt.rc.ToolBudget,
		Emitter:                          rt.emitter,
		PendingMemoryWrites:              rt.rc.PendingMemoryWrites,
		RuntimeSnapshot:                  rt.rc.Runtime,
		Channel:                          rt.rc.ChannelToolSurface,
	}
	result := rt.rc.ToolExecutor.Execute(rt.ctx, toolName, args, execCtx, "")

	for _, ev := range result.Events {
		if err := rt.yield(ev); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
	}

	if result.Error != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(result.Error.Message))
		return 2
	}

	encoded, err := json.Marshal(result.ResultJSON)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(encoded)))
	L.Push(lua.LNil)
	return 2
}

// context.get(key) -> value（string 直接返回，其他类型 JSON marshal）
// 额外支持 "system_prompt" 和 "messages" 读取 RunContext 字段。
func (rt *luaRuntime) contextGet(L *lua.LState) int {
	key := L.CheckString(1)

	// RunContext 级字段优先
	switch key {
	case "system_prompt":
		L.Push(lua.LString(rt.rc.SystemPrompt))
		return 1
	case "messages":
		msgs := make([]map[string]any, 0, len(rt.rc.Messages))
		for _, m := range rt.rc.Messages {
			text := ""
			for _, p := range m.Content {
				text += p.Text
			}
			msgs = append(msgs, map[string]any{"role": m.Role, "content": text})
		}
		encoded, err := json.Marshal(msgs)
		if err != nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LString(string(encoded)))
		return 1
	}

	if rt.rc.InputJSON == nil {
		L.Push(lua.LNil)
		return 1
	}
	val, ok := rt.rc.InputJSON[key]
	if !ok {
		L.Push(lua.LNil)
		return 1
	}
	switch v := val.(type) {
	case string:
		L.Push(lua.LString(v))
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LString(string(encoded)))
		}
	}
	return 1
}

// context.set_output(text) — 设置脚本的最终输出文本。
func (rt *luaRuntime) contextSetOutput(L *lua.LState) int {
	rt.output = L.CheckString(1)
	return 0
}

// context.emit(event_type, data) -> (ok, err)
// data 接受 Lua table（自动转 map）或 JSON string。
func (rt *luaRuntime) contextEmit(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	eventType := L.CheckString(1)
	dataArg := L.CheckAny(2)

	var data map[string]any
	switch v := dataArg.(type) {
	case *lua.LTable:
		raw := luaToGoValue(v)
		if m, ok := raw.(map[string]any); ok {
			data = m
		} else {
			data = map[string]any{}
		}
	case lua.LString:
		if err := json.Unmarshal([]byte(string(v)), &data); err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(fmt.Sprintf("context.emit: invalid JSON: %s", err.Error())))
			return 2
		}
	default:
		data = map[string]any{}
	}

	if err := rt.yield(rt.emitter.Emit(eventType, data, nil, nil)); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) parseSpawnRequest(tbl *lua.LTable) (subagentctl.SpawnRequest, error) {
	if err := ensureLuaTableKeys(tbl, "agent.spawn", map[string]struct{}{
		"persona_id":   {},
		"input":        {},
		"context_mode": {},
		"role":         {},
		"nickname":     {},
		"inherit":      {},
	}); err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	personaID, err := luaRequiredString(tbl.RawGetString("persona_id"), "agent.spawn: persona_id")
	if err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	input, err := luaRequiredString(tbl.RawGetString("input"), "agent.spawn: input")
	if err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	contextMode := data.SubAgentContextModeIsolated
	if raw := tbl.RawGetString("context_mode"); raw != lua.LNil {
		contextMode, err = luaRequiredString(raw, "agent.spawn: context_mode")
		if err != nil {
			return subagentctl.SpawnRequest{}, err
		}
	}
	role, err := luaOptionalStringPtr(tbl.RawGetString("role"), "agent.spawn: role")
	if err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	nickname, err := luaOptionalStringPtr(tbl.RawGetString("nickname"), "agent.spawn: nickname")
	if err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	inherit, err := parseLuaSpawnInherit(tbl.RawGetString("inherit"))
	if err != nil {
		return subagentctl.SpawnRequest{}, err
	}
	return subagentctl.SpawnRequest{
		PersonaID:   personaID,
		Role:        role,
		Nickname:    nickname,
		ContextMode: contextMode,
		Inherit:     inherit,
		Input:       input,
		ParentContext: subagentctl.SpawnParentContext{
			ToolAllowlist: append([]string(nil), sortedToolNames(rt.rc.AllowlistSet)...),
			ToolDenylist:  append([]string(nil), rt.rc.ToolDenylist...),
			RouteID:       routeIDFromRunContext(rt.rc),
			Model:         modelFromRunContext(rt.rc),
			ProfileRef:    strings.TrimSpace(rt.rc.ProfileRef),
			WorkspaceRef:  strings.TrimSpace(rt.rc.WorkspaceRef),
			EnabledSkills: append([]skillstore.ResolvedSkill(nil), rt.rc.EnabledSkills...),
			MemoryScope:   subagentctl.MemoryScopeSameUser,
		},
	}, nil
}

func parseLuaSpawnInherit(value lua.LValue) (subagentctl.SpawnInheritRequest, error) {
	if value == lua.LNil {
		return subagentctl.SpawnInheritRequest{}, nil
	}
	tbl, ok := value.(*lua.LTable)
	if !ok {
		return subagentctl.SpawnInheritRequest{}, fmt.Errorf("agent.spawn: inherit must be a table")
	}
	if err := ensureLuaTableKeys(tbl, "agent.spawn: inherit", map[string]struct{}{
		"messages":     {},
		"attachments":  {},
		"workspace":    {},
		"skills":       {},
		"runtime":      {},
		"memory_scope": {},
		"message_ids":  {},
	}); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	inherit := subagentctl.SpawnInheritRequest{}
	var err error
	if inherit.Messages, err = luaOptionalBoolPtr(tbl.RawGetString("messages"), "agent.spawn: inherit.messages"); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	if inherit.Attachments, err = luaOptionalBoolPtr(tbl.RawGetString("attachments"), "agent.spawn: inherit.attachments"); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	if inherit.Workspace, err = luaOptionalBoolPtr(tbl.RawGetString("workspace"), "agent.spawn: inherit.workspace"); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	if inherit.Skills, err = luaOptionalBoolPtr(tbl.RawGetString("skills"), "agent.spawn: inherit.skills"); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	if inherit.Runtime, err = luaOptionalBoolPtr(tbl.RawGetString("runtime"), "agent.spawn: inherit.runtime"); err != nil {
		return subagentctl.SpawnInheritRequest{}, err
	}
	if raw := tbl.RawGetString("memory_scope"); raw != lua.LNil {
		inherit.MemoryScope, err = luaRequiredString(raw, "agent.spawn: inherit.memory_scope")
		if err != nil {
			return subagentctl.SpawnInheritRequest{}, err
		}
	}
	if raw := tbl.RawGetString("message_ids"); raw != lua.LNil {
		messageIDsTable, ok := raw.(*lua.LTable)
		if !ok {
			return subagentctl.SpawnInheritRequest{}, fmt.Errorf("agent.spawn: inherit.message_ids must be a non-empty array")
		}
		if messageIDsTable.Len() == 0 {
			return subagentctl.SpawnInheritRequest{}, fmt.Errorf("agent.spawn: inherit.message_ids must be a non-empty array")
		}
		seen := map[uuid.UUID]struct{}{}
		for i := 1; i <= messageIDsTable.Len(); i++ {
			messageID, err := parseLuaSubAgentID(messageIDsTable.RawGetInt(i), "agent.spawn: inherit.message_ids")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			if _, ok := seen[messageID]; ok {
				continue
			}
			seen[messageID] = struct{}{}
			inherit.MessageIDs = append(inherit.MessageIDs, messageID)
		}
	}
	return inherit, nil
}

func parseLuaSendOptions(value lua.LValue) (bool, error) {
	if value == lua.LNil {
		return false, nil
	}
	tbl, ok := value.(*lua.LTable)
	if !ok {
		return false, fmt.Errorf("agent.send: opts must be a table")
	}
	if err := ensureLuaTableKeys(tbl, "agent.send", map[string]struct{}{"interrupt": {}}); err != nil {
		return false, err
	}
	raw := tbl.RawGetString("interrupt")
	if raw == lua.LNil {
		return false, nil
	}
	interrupt, ok := raw.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("agent.send: opts.interrupt must be a boolean")
	}
	return bool(interrupt), nil
}

func parseLuaTimeout(value lua.LValue, field string) (time.Duration, error) {
	if value == lua.LNil {
		return 0, nil
	}
	number, ok := value.(lua.LNumber)
	if !ok {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	ms := float64(number)
	if ms <= 0 || ms != float64(int(ms)) {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	return time.Duration(int(ms)) * time.Millisecond, nil
}

func parseLuaSubAgentID(value lua.LValue, field string) (uuid.UUID, error) {
	text, err := luaRequiredString(value, field)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid UUID", field)
	}
	return id, nil
}

func luaRequiredString(value lua.LValue, field string) (string, error) {
	text, ok := value.(lua.LString)
	if !ok || strings.TrimSpace(string(text)) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", field)
	}
	return strings.TrimSpace(string(text)), nil
}

func luaOptionalStringPtr(value lua.LValue, field string) (*string, error) {
	if value == lua.LNil {
		return nil, nil
	}
	text, ok := value.(lua.LString)
	if !ok {
		return nil, fmt.Errorf("%s must be a string", field)
	}
	trimmed := strings.TrimSpace(string(text))
	if trimmed == "" {
		return nil, nil
	}
	return &trimmed, nil
}

func luaOptionalBoolPtr(value lua.LValue, field string) (*bool, error) {
	if value == lua.LNil {
		return nil, nil
	}
	boolean, ok := value.(lua.LBool)
	if !ok {
		return nil, fmt.Errorf("%s must be a boolean", field)
	}
	parsed := bool(boolean)
	return &parsed, nil
}

func ensureLuaTableKeys(tbl *lua.LTable, field string, allowed map[string]struct{}) error {
	var err error
	tbl.ForEach(func(key lua.LValue, _ lua.LValue) {
		if err != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			err = fmt.Errorf("%s keys must be strings", field)
			return
		}
		if _, ok := allowed[string(name)]; !ok {
			err = fmt.Errorf("%s has unknown field %q", field, string(name))
		}
	})
	return err
}

func statusSnapshotToLuaTable(L *lua.LState, snapshot subagentctl.StatusSnapshot) *lua.LTable {
	tbl := L.NewTable()
	tbl.RawSetString("id", lua.LString(snapshot.SubAgentID.String()))
	tbl.RawSetString("parent_run_id", lua.LString(snapshot.ParentRunID.String()))
	tbl.RawSetString("root_run_id", lua.LString(snapshot.RootRunID.String()))
	tbl.RawSetString("depth", lua.LNumber(snapshot.Depth))
	setLuaStringField(tbl, "status", snapshot.Status)
	setLuaOptionalStringField(tbl, "role", snapshot.Role)
	setLuaOptionalStringField(tbl, "persona_id", snapshot.PersonaID)
	setLuaOptionalStringField(tbl, "nickname", snapshot.Nickname)
	setLuaStringField(tbl, "context_mode", snapshot.ContextMode)
	setLuaOptionalUUIDField(tbl, "current_run_id", snapshot.CurrentRunID)
	setLuaOptionalUUIDField(tbl, "last_completed_run_id", snapshot.LastCompletedRunID)
	setLuaOptionalStringField(tbl, "last_output_ref", snapshot.LastOutputRef)
	setLuaOptionalStringField(tbl, "output", snapshot.LastOutput)
	setLuaOptionalStringField(tbl, "last_error", snapshot.LastError)
	if snapshot.LastEventSeq != nil {
		tbl.RawSetString("last_event_seq", lua.LNumber(*snapshot.LastEventSeq))
	}
	setLuaOptionalStringField(tbl, "last_event_type", snapshot.LastEventType)
	setLuaOptionalTimeField(tbl, "started_at", snapshot.StartedAt)
	setLuaOptionalTimeField(tbl, "completed_at", snapshot.CompletedAt)
	setLuaOptionalTimeField(tbl, "closed_at", snapshot.ClosedAt)
	return tbl
}

func setLuaStringField(tbl *lua.LTable, key string, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	tbl.RawSetString(key, lua.LString(trimmed))
}

func setLuaOptionalStringField(tbl *lua.LTable, key string, value *string) {
	if value == nil {
		return
	}
	setLuaStringField(tbl, key, *value)
}

func setLuaOptionalUUIDField(tbl *lua.LTable, key string, value *uuid.UUID) {
	if value == nil {
		return
	}
	tbl.RawSetString(key, lua.LString(value.String()))
}

func setLuaOptionalTimeField(tbl *lua.LTable, key string, value *time.Time) {
	if value == nil {
		return
	}
	tbl.RawSetString(key, lua.LString(value.UTC().Format(time.RFC3339Nano)))
}

func parseMaxTokensOption(opts *lua.LTable) *int {
	if opts == nil {
		return nil
	}
	if raw := opts.RawGetString("max_tokens"); raw != lua.LNil {
		if number, ok := raw.(lua.LNumber); ok {
			value := int(number)
			return &value
		}
	}
	return nil
}

func parseAgentMessages(systemPrompt string, messagesArg lua.LValue, bindingName string) ([]llm.Message, error) {
	messages := []llm.Message{
		{Role: "system", Content: []llm.TextPart{{Text: systemPrompt}}},
	}

	switch value := messagesArg.(type) {
	case lua.LString:
		messages = append(messages, llm.Message{
			Role:    "user",
			Content: []llm.TextPart{{Text: string(value)}},
		})
		return messages, nil
	case *lua.LTable:
		n := value.Len()
		for i := 1; i <= n; i++ {
			item := value.RawGetInt(i)
			tbl, ok := item.(*lua.LTable)
			if !ok {
				continue
			}
			role := ""
			if rawRole, ok := tbl.RawGetString("role").(lua.LString); ok {
				role = string(rawRole)
			}
			content := ""
			if rawContent, ok := tbl.RawGetString("content").(lua.LString); ok {
				content = string(rawContent)
			}
			if strings.TrimSpace(role) == "" || strings.TrimSpace(content) == "" {
				continue
			}
			messages = append(messages, llm.Message{
				Role:    role,
				Content: []llm.TextPart{{Text: content}},
			})
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("%s: messages must be a string or table", bindingName)
	}
}

func usageFromRunEvent(data map[string]any) *llm.Usage {
	if data == nil {
		return nil
	}
	rawUsage, ok := data["usage"]
	if !ok {
		return nil
	}
	usageMap, ok := rawUsage.(map[string]any)
	if !ok {
		return nil
	}

	usage := llm.Usage{
		InputTokens:              intPtrFromAny(usageMap["input_tokens"]),
		OutputTokens:             intPtrFromAny(usageMap["output_tokens"]),
		TotalTokens:              intPtrFromAny(usageMap["total_tokens"]),
		CacheCreationInputTokens: intPtrFromAny(usageMap["cache_creation_input_tokens"]),
		CacheReadInputTokens:     intPtrFromAny(usageMap["cache_read_input_tokens"]),
		CachedTokens:             intPtrFromAny(usageMap["cached_tokens"]),
	}
	if len(usage.ToJSON()) == 0 {
		return nil
	}
	return &usage
}

func intPtrFromAny(value any) *int {
	switch typed := value.(type) {
	case int:
		v := typed
		return &v
	case int8:
		v := int(typed)
		return &v
	case int16:
		v := int(typed)
		return &v
	case int32:
		v := int(typed)
		return &v
	case int64:
		v := int(typed)
		return &v
	case uint:
		v := int(typed)
		return &v
	case uint8:
		v := int(typed)
		return &v
	case uint16:
		v := int(typed)
		return &v
	case uint32:
		v := int(typed)
		return &v
	case uint64:
		v := int(typed)
		return &v
	case float32:
		v := int(typed)
		return &v
	case float64:
		v := int(typed)
		return &v
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return nil
		}
		v := int(parsed)
		return &v
	default:
		return nil
	}
}

// agent.generate(system_prompt, user_message, [opts]) -> (output, err)
// 轻量级 LLM 调用，不创建子 Run，不 yield 事件。
// opts: {max_tokens=number}
func (rt *luaRuntime) agentGenerate(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.generate not available: gateway not initialized"))
		return 2
	}

	sysPrompt := L.CheckString(1)
	userMessage := L.CheckString(2)

	req := llm.Request{
		Model: rt.rc.SelectedRoute.Route.Model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: sysPrompt}}},
			{Role: "user", Content: []llm.TextPart{{Text: userMessage}}},
		},
	}
	req.MaxOutputTokens = parseMaxTokensOption(L.OptTable(3, nil))

	var chunks []string
	var streamFailed *llm.StreamRunFailed
	sentinel := fmt.Errorf("stop")

	err := rt.rc.Gateway.Stream(rt.ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunFailed:
			streamFailed = &typed
			return sentinel
		case llm.StreamRunCompleted:
			rt.mergeUsage(typed.Usage)
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	if streamFailed != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(streamFailed.Error.Message))
		return 2
	}

	L.Push(lua.LString(strings.TrimSpace(strings.Join(chunks, ""))))
	L.Push(lua.LNil)
	return 2
}

// agent.stream(system_prompt, messages, [opts]) -> (output, err)
// 流式 LLM 调用，每个 delta 通过 yield 推送 message.delta 到前端。
// messages: string（单条 user 消息）或 table（{role, content} 数组）。
// opts: {max_tokens=number}
func (rt *luaRuntime) agentStream(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.stream not available: gateway not initialized"))
		return 2
	}

	sysPrompt := L.CheckString(1)
	messagesArg := L.CheckAny(2)
	messages, parseErr := parseAgentMessages(sysPrompt, messagesArg, "agent.stream")
	if parseErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(parseErr.Error()))
		return 2
	}
	maxTokens := parseMaxTokensOption(L.OptTable(3, nil))

	outputText, _, streamFailed, streamErr := rt.streamWithGateway(
		rt.rc.Gateway,
		rt.rc.SelectedRoute.Route.Model,
		messages,
		maxTokens,
		true,
	)
	if streamErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(streamErr.Error()))
		return 2
	}
	if streamFailed != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(streamFailed.Error.Message))
		return 2
	}

	L.Push(lua.LString(outputText))
	L.Push(lua.LNil)
	return 2
}

// agent.stream_route(route_id, system_prompt, messages, [opts]) -> (output, err)
// 与 agent.stream 类似，但允许按 route_id 指定输出模型。
func (rt *luaRuntime) agentStreamRoute(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.stream_route not available: gateway not initialized"))
		return 2
	}

	routeID := strings.TrimSpace(L.OptString(1, ""))
	sysPrompt := L.CheckString(2)
	messagesArg := L.CheckAny(3)
	messages, parseErr := parseAgentMessages(sysPrompt, messagesArg, "agent.stream_route")
	if parseErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(parseErr.Error()))
		return 2
	}
	maxTokens := parseMaxTokensOption(L.OptTable(4, nil))

	gateway := rt.rc.Gateway
	model := rt.rc.SelectedRoute.Route.Model
	if routeID != "" {
		if rt.rc.ResolveGatewayForRouteID == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("route_resolve_failed: resolver not initialized"))
			return 2
		}
		resolvedGateway, resolvedRoute, resolveErr := rt.rc.ResolveGatewayForRouteID(rt.ctx, routeID)
		if resolveErr != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("route_resolve_failed: " + resolveErr.Error()))
			return 2
		}
		if resolvedGateway == nil || resolvedRoute == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("route_resolve_failed: resolved gateway or route is nil"))
			return 2
		}
		gateway = resolvedGateway
		model = resolvedRoute.Route.Model
	}

	outputText, streamStarted, streamFailed, streamErr := rt.streamWithGateway(
		gateway,
		model,
		messages,
		maxTokens,
		true,
	)
	if streamErr != nil {
		if streamStarted {
			errorClass := llm.ErrorClassInternalError
			if emitErr := rt.yield(rt.emitter.Emit("run.failed", map[string]any{
				"error_class": errorClass,
				"message":     streamErr.Error(),
			}, nil, &errorClass)); emitErr != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(emitErr.Error()))
				return 2
			}
			rt.loopTerminal = true
			L.Push(lua.LNil)
			L.Push(lua.LString("stream_terminal_failed: " + streamErr.Error()))
			return 2
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(streamErr.Error()))
		return 2
	}
	if streamFailed != nil {
		if streamStarted {
			if emitErr := rt.yield(rt.emitter.Emit("run.failed", streamFailed.ToDataJSON(), nil, stringPtr(streamFailed.Error.ErrorClass))); emitErr != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(emitErr.Error()))
				return 2
			}
			rt.loopTerminal = true
			L.Push(lua.LNil)
			L.Push(lua.LString("stream_terminal_failed: " + streamFailed.Error.Message))
			return 2
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(streamFailed.Error.Message))
		return 2
	}

	L.Push(lua.LString(outputText))
	L.Push(lua.LNil)
	return 2
}

// agent.stream_agent(agent_name, system_prompt, messages, [opts]) -> (output, err)
// 与 agent.stream_route 类似，但按 Agent 配置名称解析输出模型。
func (rt *luaRuntime) agentStreamAgent(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.stream_agent not available: gateway not initialized"))
		return 2
	}

	agentName := strings.TrimSpace(L.OptString(1, ""))
	sysPrompt := L.CheckString(2)
	messagesArg := L.CheckAny(3)
	messages, parseErr := parseAgentMessages(sysPrompt, messagesArg, "agent.stream_agent")
	if parseErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(parseErr.Error()))
		return 2
	}
	maxTokens := parseMaxTokensOption(L.OptTable(4, nil))

	gateway := rt.rc.Gateway
	model := rt.rc.SelectedRoute.Route.Model
	if agentName != "" {
		if rt.rc.ResolveGatewayForAgentName == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("agent_resolve_failed: resolver not initialized"))
			return 2
		}
		resolvedGateway, resolvedRoute, resolveErr := rt.rc.ResolveGatewayForAgentName(rt.ctx, agentName)
		if resolveErr != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("agent_resolve_failed: " + resolveErr.Error()))
			return 2
		}
		if resolvedGateway == nil || resolvedRoute == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("agent_resolve_failed: resolved gateway or route is nil"))
			return 2
		}
		gateway = resolvedGateway
		model = resolvedRoute.Route.Model
	}

	outputText, streamStarted, streamFailed, streamErr := rt.streamWithGateway(
		gateway,
		model,
		messages,
		maxTokens,
		true,
	)
	if streamErr != nil {
		if streamStarted {
			errorClass := llm.ErrorClassInternalError
			if emitErr := rt.yield(rt.emitter.Emit("run.failed", map[string]any{
				"error_class": errorClass,
				"message":     streamErr.Error(),
			}, nil, &errorClass)); emitErr != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(emitErr.Error()))
				return 2
			}
			rt.loopTerminal = true
			L.Push(lua.LNil)
			L.Push(lua.LString("stream_terminal_failed: " + streamErr.Error()))
			return 2
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(streamErr.Error()))
		return 2
	}
	if streamFailed != nil {
		if streamStarted {
			if emitErr := rt.yield(rt.emitter.Emit("run.failed", streamFailed.ToDataJSON(), nil, stringPtr(streamFailed.Error.ErrorClass))); emitErr != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(emitErr.Error()))
				return 2
			}
			rt.loopTerminal = true
			L.Push(lua.LNil)
			L.Push(lua.LString("stream_terminal_failed: " + streamFailed.Error.Message))
			return 2
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(streamFailed.Error.Message))
		return 2
	}

	L.Push(lua.LString(outputText))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) streamWithGateway(
	gateway llm.Gateway,
	model string,
	messages []llm.Message,
	maxTokens *int,
	emitDelta bool,
) (string, bool, *llm.StreamRunFailed, error) {
	req := llm.Request{
		Model:           model,
		Messages:        messages,
		MaxOutputTokens: maxTokens,
	}

	var chunks []string
	var streamFailed *llm.StreamRunFailed
	streamStarted := false
	sentinel := fmt.Errorf("stop")

	err := gateway.Stream(rt.ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta == "" {
				return nil
			}
			chunks = append(chunks, typed.ContentDelta)
			if emitDelta {
				streamStarted = true
				if yieldErr := rt.yield(rt.emitter.Emit("message.delta", typed.ToDataJSON(), nil, nil)); yieldErr != nil {
					return yieldErr
				}
			}
			return nil
		case llm.StreamRunFailed:
			rt.mergeUsage(typed.Usage)
			streamFailed = &typed
			return sentinel
		case llm.StreamRunCompleted:
			rt.mergeUsage(typed.Usage)
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		return "", streamStarted, nil, err
	}
	return strings.Join(chunks, ""), streamStarted, streamFailed, nil
}

// agent.loop(system_prompt, messages, [opts]) -> (ok, err)
// 完整 agent 循环：LLM 自主决定调用哪些工具，工具执行后继续对话，
// 直到 LLM 输出最终文本或达到迭代上限。
// 与 agent.stream 的区别：此方法将可用工具传递给 LLM 并自动处理 tool calling loop。
func (rt *luaRuntime) agentLoop(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.loop not available: gateway not initialized"))
		return 2
	}

	sysPrompt := L.CheckString(1)
	messagesArg := L.CheckAny(2)
	messages, parseErr := parseAgentMessages(sysPrompt, messagesArg, "agent.loop")
	if parseErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(parseErr.Error()))
		return 2
	}
	maxTokens := parseMaxTokensOption(L.OptTable(3, nil))
	if _, runErr := rt.runAgentLoop(messages, maxTokens, false, true, false); runErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(runErr.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	L.Push(lua.LNil)
	return 2
}

// agent.loop_capture(system_prompt, messages, [opts]) -> (captured_text, err)
// 完整 agent 循环 + 工具调用，默认不透传普通文本 delta，返回捕获到的文本。
func (rt *luaRuntime) agentLoopCapture(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}
	if rt.rc.Gateway == nil || rt.rc.SelectedRoute == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.loop_capture not available: gateway not initialized"))
		return 2
	}

	sysPrompt := L.CheckString(1)
	messagesArg := L.CheckAny(2)
	messages, parseErr := parseAgentMessages(sysPrompt, messagesArg, "agent.loop_capture")
	if parseErr != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(parseErr.Error()))
		return 2
	}
	maxTokens := parseMaxTokensOption(L.OptTable(3, nil))
	capturedText, runErr := rt.runAgentLoop(messages, maxTokens, true, false, true)
	if runErr != nil {
		if !rt.loopTerminal {
			errorClass := llm.ErrorClassInternalError
			if emitErr := rt.yield(rt.emitter.Emit("run.failed", map[string]any{
				"error_class": errorClass,
				"message":     runErr.Error(),
			}, nil, &errorClass)); emitErr != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(emitErr.Error()))
				return 2
			}
			rt.loopTerminal = true
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(runErr.Error()))
		return 2
	}

	L.Push(lua.LString(capturedText))
	L.Push(lua.LNil)
	return 2
}

func (rt *luaRuntime) runAgentLoop(
	messages []llm.Message,
	maxTokens *int,
	capturePlainText bool,
	terminalOnCompleted bool,
	returnFailureError bool,
) (string, error) {
	request := llm.Request{
		Model:           rt.rc.SelectedRoute.Route.Model,
		Messages:        messages,
		Tools:           append([]llm.ToolSpec{}, rt.rc.FinalSpecs...),
		MaxOutputTokens: maxTokens,
		ReasoningMode:   rt.rc.ReasoningMode,
	}

	maxIter := rt.rc.ReasoningIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	runCtx := agent.RunContext{
		RunID:                  rt.rc.Run.ID,
		AccountID:              &rt.rc.Run.AccountID,
		UserID:                 rt.rc.UserID,
		AgentID:                agentIDFromPersona(rt.rc),
		ThreadID:               &rt.rc.Run.ThreadID,
		ProjectID:              rt.rc.Run.ProjectID,
		ProfileRef:             rt.rc.ProfileRef,
		WorkspaceRef:           rt.rc.WorkspaceRef,
		WorkDir:                rt.rc.WorkDir,
		EnabledSkills:          append([]skillstore.ResolvedSkill(nil), rt.rc.EnabledSkills...),
		ToolAllowlist:          sortedToolNames(rt.rc.AllowlistSet),
		ToolDenylist:           append([]string(nil), rt.rc.ToolDenylist...),
		RouteID:                routeIDFromRunContext(rt.rc),
		Model:                  modelFromRunContext(rt.rc),
		MemoryScope:            "same_user",
		TraceID:                rt.rc.TraceID,
		InputJSON:              rt.rc.InputJSON,
		ReasoningIterations:    maxIter,
		ToolContinuationBudget: rt.rc.ToolContinuationBudget,
		ToolExecutor:           rt.rc.ToolExecutor,
		ToolTimeoutMs:          rt.rc.ToolTimeoutMs,
		ToolBudget:             rt.rc.ToolBudget,
		PerToolSoftLimits:      rt.rc.PerToolSoftLimits,
		MaxCostMicros:          rt.rc.MaxCostMicros,
		MaxTotalOutputTokens:   rt.rc.MaxTotalOutputTokens,
		PendingMemoryWrites:    rt.rc.PendingMemoryWrites,
		Runtime:                rt.rc.Runtime,
		LlmRetryMaxAttempts:    rt.rc.LlmRetryMaxAttempts,
		LlmRetryBaseDelayMs:    rt.rc.LlmRetryBaseDelayMs,
		UserPromptScanFunc:     rt.rc.UserPromptScanFunc,
		ToolOutputScanFunc:     rt.rc.ToolOutputScanFunc,
		Channel:                rt.rc.ChannelToolSurface,
		CancelSignal: func() bool {
			return rt.ctx.Err() != nil
		},
		StreamThinking: rt.rc.StreamThinking,
	}

	capturedChunks := make([]string, 0, 16)
	terminalFailureMessage := ""
	wrappedYield := func(ev events.RunEvent) error {
		switch ev.Type {
		case "run.completed":
			rt.mergeUsage(usageFromRunEvent(ev.DataJSON))
			if terminalOnCompleted {
				rt.loopTerminal = true
			}
			return nil
		case "run.failed":
			rt.loopTerminal = true
			terminalFailureMessage = runFailedMessage(ev.DataJSON)
			return rt.yield(ev)
		case "message.delta":
			if !capturePlainText {
				return rt.yield(ev)
			}
			channel, _ := ev.DataJSON["channel"].(string)
			if channel != "" {
				return rt.yield(ev)
			}
			if text, ok := ev.DataJSON["content_delta"].(string); ok && text != "" {
				capturedChunks = append(capturedChunks, text)
			}
			return nil
		case "tool.result":
			if capturePlainText {
				if result, ok := ev.DataJSON["result"]; ok {
					toolName, _ := ev.DataJSON["tool_name"].(string)
					if resultBytes, err := json.Marshal(result); err == nil {
						capturedChunks = append(capturedChunks, fmt.Sprintf("\n[tool_result: %s]\n%s\n[/tool_result]\n", toolName, string(resultBytes)))
					}
				}
			}
			return rt.yield(ev)
		default:
			return rt.yield(ev)
		}
	}

	loop := agent.NewLoop(rt.rc.Gateway, rt.rc.ToolExecutor)
	if err := loop.Run(rt.ctx, runCtx, request, rt.emitter, wrappedYield); err != nil {
		return "", err
	}
	if returnFailureError && terminalFailureMessage != "" {
		return "", fmt.Errorf("%s", terminalFailureMessage)
	}
	return strings.Join(capturedChunks, ""), nil
}

func runFailedMessage(data map[string]any) string {
	if data == nil {
		return "agent loop failed"
	}
	if message, ok := data["message"].(string); ok && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	return "agent loop failed"
}

// tools.call_parallel(calls) -> (results, errors)
// calls: {{name="tool_name", args='{"key":"val"}'}, ...}
// 并行执行所有 tool 调用，事件通过 mutex 序列化推送。
func (rt *luaRuntime) toolsCallParallel(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	callsTable := L.CheckTable(1)
	n := callsTable.Len()
	if n == 0 {
		L.Push(L.NewTable())
		L.Push(L.NewTable())
		return 2
	}

	if rt.rc.ToolExecutor == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("tools.call_parallel not available: tool executor not initialized"))
		return 2
	}
	limit := rt.rc.MaxParallelTasks
	if limit <= 0 {
		limit = 32
	}
	if n > limit {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("tools.call_parallel: count %d exceeds limit %d", n, limit)))
		return 2
	}

	type callEntry struct {
		name    string
		args    map[string]any
		argsRaw string
	}

	calls := make([]callEntry, n)
	for i := 0; i < n; i++ {
		v := callsTable.RawGetInt(i + 1)
		tbl, ok := v.(*lua.LTable)
		if !ok {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("calls[%d] must be a table", i+1)))
			return 2
		}
		nameLV, ok := tbl.RawGetString("name").(lua.LString)
		if !ok || string(nameLV) == "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("calls[%d].name must be a non-empty string", i+1)))
			return 2
		}
		argsLV, ok := tbl.RawGetString("args").(lua.LString)
		if !ok {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("calls[%d].args must be a JSON string", i+1)))
			return 2
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(string(argsLV)), &args); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("calls[%d].args: invalid JSON: %s", i+1, err.Error())))
			return 2
		}
		calls[i] = callEntry{name: string(nameLV), args: args, argsRaw: string(argsLV)}
	}

	type callResult struct {
		resultJSON string
		err        error
	}
	results := make([]callResult, n)

	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(n)

	for i, c := range calls {
		i, c := i, c
		go func() {
			defer wg.Done()
			execCtx := tools.ExecutionContext{
				RunID:                            rt.rc.Run.ID,
				TraceID:                          rt.rc.TraceID,
				AccountID:                        &rt.rc.Run.AccountID,
				ThreadID:                         &rt.rc.Run.ThreadID,
				ProjectID:                        rt.rc.Run.ProjectID,
				UserID:                           rt.rc.UserID,
				ProfileRef:                       rt.rc.ProfileRef,
				WorkspaceRef:                     rt.rc.WorkspaceRef,
				EnabledSkills:                    append([]skillstore.ResolvedSkill(nil), rt.rc.EnabledSkills...),
				ToolAllowlist:                    sortedToolNames(rt.rc.AllowlistSet),
				ToolDenylist:                     append([]string(nil), rt.rc.ToolDenylist...),
				ActiveToolProviderConfigsByGroup: copyProviderConfigMap(rt.rc.ActiveToolProviderConfigsByGroup),
				RouteID:                          routeIDFromRunContext(rt.rc),
				Model:                            modelFromRunContext(rt.rc),
				MemoryScope:                      "same_user",
				AgentID:                          agentIDFromPersona(rt.rc),
				TimeoutMs:                        rt.rc.ToolTimeoutMs,
				Budget:                           rt.rc.ToolBudget,
				Emitter:                          rt.emitter,
				PendingMemoryWrites:              rt.rc.PendingMemoryWrites,
				RuntimeSnapshot:                  rt.rc.Runtime,
				Channel:                          rt.rc.ChannelToolSurface,
			}
			result := rt.rc.ToolExecutor.Execute(rt.ctx, c.name, c.args, execCtx, "")

			// 序列化事件推送
			mu.Lock()
			for _, ev := range result.Events {
				_ = rt.yield(ev)
			}
			// 补发 tool.call（若 executor 未发射）
			emittedCall := false
			for _, ev := range result.Events {
				if ev.Type == "tool.call" {
					emittedCall = true
					break
				}
			}
			if !emittedCall {
				_ = rt.yield(rt.emitter.Emit("tool.call", map[string]any{
					"tool_name": c.name,
					"arguments": c.args,
				}, stringPtr(c.name), nil))
			}
			// 发射 tool.result
			var errorClass *string
			if result.Error != nil {
				errorClass = stringPtr(result.Error.ErrorClass)
			}
			resultData := map[string]any{
				"tool_name": c.name,
			}
			if result.ResultJSON != nil {
				resultData["result"] = result.ResultJSON
			}
			if result.Error != nil {
				resultData["error"] = map[string]any{
					"error_class": result.Error.ErrorClass,
					"message":     result.Error.Message,
				}
			}
			_ = rt.yield(rt.emitter.Emit("tool.result", resultData, stringPtr(c.name), errorClass))
			mu.Unlock()

			if result.Error != nil {
				results[i] = callResult{err: fmt.Errorf("%s", result.Error.Message)}
			} else {
				encoded, err := json.Marshal(result.ResultJSON)
				if err != nil {
					results[i] = callResult{err: err}
				} else {
					results[i] = callResult{resultJSON: string(encoded)}
				}
			}
		}()
	}
	wg.Wait()

	resultsTable := L.NewTable()
	errorsTable := L.NewTable()
	for i := 0; i < n; i++ {
		if results[i].err != nil {
			resultsTable.RawSetInt(i+1, lua.LNil)
			errorsTable.RawSetInt(i+1, lua.LString(results[i].err.Error()))
		} else {
			resultsTable.RawSetInt(i+1, lua.LString(results[i].resultJSON))
			errorsTable.RawSetInt(i+1, lua.LNil)
		}
	}

	L.Push(resultsTable)
	L.Push(errorsTable)
	return 2
}

// memory.search(query, [scope], [limit]) -> (results_json, err)
func (rt *luaRuntime) memorySearch(L *lua.LState) int {
	if rt.rc.MemoryProvider == nil {
		L.Push(lua.LString("[]"))
		L.Push(lua.LNil)
		return 2
	}

	query := L.CheckString(1)
	scope := memory.MemoryScopeUser
	if s := L.OptString(2, "user"); s == "agent" {
		scope = memory.MemoryScopeAgent
	}
	limit := L.OptInt(3, 5)

	ident := rt.memoryIdentity()
	hits, err := rt.rc.MemoryProvider.Find(rt.ctx, ident, scope, query, limit)
	if err != nil {
		L.Push(lua.LString("[]"))
		L.Push(lua.LString(err.Error()))
		return 2
	}

	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{
			"uri":          h.URI,
			"abstract":     h.Abstract,
			"score":        h.Score,
			"match_reason": h.MatchReason,
		})
	}
	encoded, _ := json.Marshal(results)
	L.Push(lua.LString(string(encoded)))
	L.Push(lua.LNil)
	return 2
}

// memory.read(uri, [depth]) -> (content, err)
func (rt *luaRuntime) memoryRead(L *lua.LState) int {
	if rt.rc.MemoryProvider == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("memory provider not available"))
		return 2
	}

	uri := L.CheckString(1)
	layer := memory.MemoryLayerOverview
	if d := L.OptString(2, "overview"); d == "full" {
		layer = memory.MemoryLayerRead
	}

	ident := rt.memoryIdentity()
	content, err := rt.rc.MemoryProvider.Content(rt.ctx, ident, uri, layer)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(content))
	L.Push(lua.LNil)
	return 2
}

// memory.write(category, key, content, [scope]) -> (uri, err)
func (rt *luaRuntime) memoryWrite(L *lua.LState) int {
	if rt.rc.MemoryProvider == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("memory provider not available"))
		return 2
	}

	category := L.CheckString(1)
	key := L.CheckString(2)
	content := L.CheckString(3)
	scope := memory.MemoryScopeUser
	if s := L.OptString(4, "user"); s == "agent" {
		scope = memory.MemoryScopeAgent
	}

	writable := "[" + string(scope) + "/" + category + "/" + key + "] " + content
	entry := memory.MemoryEntry{Content: writable}

	ident := rt.memoryIdentity()
	if err := rt.rc.MemoryProvider.Write(rt.ctx, ident, scope, entry); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	uri := memory.BuildURI(scope, memory.MemoryCategory(category), key)
	L.Push(lua.LString(uri))
	L.Push(lua.LNil)
	return 2
}

// memory.forget(uri) -> (ok, err)
func (rt *luaRuntime) memoryForget(L *lua.LState) int {
	if rt.rc.MemoryProvider == nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString("memory provider not available"))
		return 2
	}

	uri := L.CheckString(1)

	ident := rt.memoryIdentity()
	if err := rt.rc.MemoryProvider.Delete(rt.ctx, ident, uri); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	L.Push(lua.LNil)
	return 2
}

// memoryIdentity 从 RunContext 构造 MemoryIdentity。
func (rt *luaRuntime) memoryIdentity() memory.MemoryIdentity {
	ident := memory.MemoryIdentity{
		AccountID: rt.rc.Run.AccountID,
		AgentID:   agentIDFromPersona(rt.rc),
	}
	if rt.rc.UserID != nil {
		ident.UserID = *rt.rc.UserID
	}
	return ident
}

// json.encode(value) -> (json_string, err)
func jsonEncode(L *lua.LState) int {
	v := L.CheckAny(1)
	encoded, err := json.Marshal(luaToGoValue(v))
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(string(encoded)))
	L.Push(lua.LNil)
	return 2
}

// json.decode(json_string) -> (value, err)
func jsonDecode(L *lua.LState) int {
	s := L.CheckString(1)
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(goToLuaValue(L, v))
	L.Push(lua.LNil)
	return 2
}

// luaToGoValue 将 Lua 值递归转换为 Go 原生类型，供 json.Marshal 使用。
func luaToGoValue(v lua.LValue) any {
	switch typed := v.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(typed)
	case lua.LNumber:
		f := float64(typed)
		if f == float64(int64(f)) {
			return int64(typed)
		}
		return f
	case lua.LString:
		return string(typed)
	case *lua.LTable:
		n := typed.Len()
		// 若顺序整数键从 1 到 n 覆盖全部条目，视为数组
		if n > 0 {
			allInt := true
			typed.ForEach(func(k, _ lua.LValue) {
				if _, ok := k.(lua.LNumber); !ok {
					allInt = false
				}
			})
			if allInt {
				arr := make([]any, n)
				for i := 1; i <= n; i++ {
					arr[i-1] = luaToGoValue(typed.RawGetInt(i))
				}
				return arr
			}
		}
		obj := map[string]any{}
		typed.ForEach(func(k, val lua.LValue) {
			obj[fmt.Sprintf("%v", k)] = luaToGoValue(val)
		})
		return obj
	default:
		return fmt.Sprintf("%v", v)
	}
}

// goToLuaValue 将 Go json.Unmarshal 产出的原生类型递归转换为 Lua 值。
func goToLuaValue(L *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch typed := v.(type) {
	case bool:
		if typed {
			return lua.LTrue
		}
		return lua.LFalse
	case float64:
		return lua.LNumber(typed)
	case string:
		return lua.LString(typed)
	case []any:
		t := L.NewTable()
		for i, item := range typed {
			t.RawSetInt(i+1, goToLuaValue(L, item))
		}
		return t
	case map[string]any:
		t := L.NewTable()
		for k, item := range typed {
			L.SetField(t, k, goToLuaValue(L, item))
		}
		return t
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
}
