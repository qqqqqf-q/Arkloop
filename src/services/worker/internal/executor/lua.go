package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"

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
	L := lua.NewState()
	defer L.Close()

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

	// 脚本执行完成，emit 最终输出
	output := strings.TrimSpace(rt.output)
	if output != "" {
		delta := llm.StreamMessageDelta{ContentDelta: output, Role: "assistant"}
		if err := yield(emitter.Emit("message.delta", delta.ToDataJSON(), nil, nil)); err != nil {
			return err
		}
	}

	return yield(emitter.Emit("run.completed", map[string]any{}, nil, nil))
}

// luaRuntime 持有单次 Execute 调用的运行时状态，注册为 Lua bindings。
type luaRuntime struct {
	ctx     context.Context
	rc      *pipeline.RunContext
	emitter events.Emitter
	yield   func(events.RunEvent) error
	output  string
}

func (rt *luaRuntime) register(L *lua.LState) {
	agentTable := L.NewTable()
	L.SetField(agentTable, "run", L.NewFunction(rt.agentRun))
	L.SetField(agentTable, "run_parallel", L.NewFunction(rt.agentRunParallel))
	L.SetField(agentTable, "classify", L.NewFunction(rt.agentClassify))
	L.SetGlobal("agent", agentTable)

	toolsTable := L.NewTable()
	L.SetField(toolsTable, "call", L.NewFunction(rt.toolsCall))
	L.SetGlobal("tools", toolsTable)

	contextTable := L.NewTable()
	L.SetField(contextTable, "get", L.NewFunction(rt.contextGet))
	L.SetField(contextTable, "set_output", L.NewFunction(rt.contextSetOutput))
	L.SetGlobal("context", contextTable)

	// memory binding：MemoryProvider 非 nil 时调用真实 provider，否则返回空/错误
	memoryTable := L.NewTable()
	L.SetField(memoryTable, "search", L.NewFunction(rt.memorySearch))
	L.SetField(memoryTable, "read", L.NewFunction(rt.memoryRead))
	L.SetField(memoryTable, "write", L.NewFunction(rt.memoryWrite))
	L.SetField(memoryTable, "forget", L.NewFunction(rt.memoryForget))
	L.SetGlobal("memory", memoryTable)
}

// agent.run(skill_id, input) -> (output, err)
// 内部调用 SpawnChildRun，父 Run 挂起等待子 Run 完成。
func (rt *luaRuntime) agentRun(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	skillID := L.CheckString(1)
	input := L.CheckString(2)

	if rt.rc.SpawnChildRun == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.run not available: SpawnChildRun not initialized"))
		return 2
	}

	output, err := rt.rc.SpawnChildRun(rt.ctx, skillID, input)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(output))
	L.Push(lua.LNil)
	return 2
}

// maxParallelTasks 防止 Lua 脚本一次性爆发过多 goroutine 和 Redis 连接。
const maxParallelTasks = 32

// agent.run_parallel(tasks) -> (results, errors)
// tasks 是 Lua table，每项为 {skill="...", input="..."}，索引从 1 开始。
// 所有子任务并行执行，全部完成后返回两个等长 table：
//
//	results[i] = 输出文本（失败时为 nil）
//	errors[i]  = 错误信息（成功时为 nil）
func (rt *luaRuntime) agentRunParallel(L *lua.LState) int {
	if rt.ctx.Err() != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(rt.ctx.Err().Error()))
		return 2
	}

	if rt.rc.SpawnChildRun == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("agent.run_parallel not available: SpawnChildRun not initialized"))
		return 2
	}

	tasksTable := L.CheckTable(1)
	n := tasksTable.Len()

	if n > maxParallelTasks {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("agent.run_parallel: task count %d exceeds limit %d", n, maxParallelTasks)))
		return 2
	}

	type taskEntry struct {
		skillID string
		input   string
	}

	tasks := make([]taskEntry, n)
	for i := 0; i < n; i++ {
		v := tasksTable.RawGetInt(i + 1)
		tbl, ok := v.(*lua.LTable)
		if !ok {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("tasks[%d] must be a table with skill and input fields", i+1)))
			return 2
		}
		skillLV, ok := tbl.RawGetString("skill").(lua.LString)
		if !ok || string(skillLV) == "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("tasks[%d].skill must be a non-empty string", i+1)))
			return 2
		}
		inputLV, ok := tbl.RawGetString("input").(lua.LString)
		if !ok {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("tasks[%d].input must be a string", i+1)))
			return 2
		}
		tasks[i] = taskEntry{skillID: string(skillLV), input: string(inputLV)}
	}

	skillIDs := make([]string, n)
	for i, t := range tasks {
		skillIDs[i] = t.skillID
	}
	if err := rt.yield(rt.emitter.Emit("agent.parallel_dispatch", map[string]any{
		"task_count": n,
		"skill_ids":  skillIDs,
	}, nil, nil)); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	outputs := make([]string, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i, t := range tasks {
		i, t := i, t
		go func() {
			defer wg.Done()
			out, err := rt.rc.SpawnChildRun(rt.ctx, t.skillID, t.input)
			outputs[i] = out
			errs[i] = err
		}()
	}
	wg.Wait()

	successCount := 0
	for _, e := range errs {
		if e == nil {
			successCount++
		}
	}
	if err := rt.yield(rt.emitter.Emit("agent.parallel_complete", map[string]any{
		"task_count":    n,
		"success_count": successCount,
		"error_count":   n - successCount,
	}, nil, nil)); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	resultsTable := L.NewTable()
	errorsTable := L.NewTable()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			resultsTable.RawSetInt(i+1, lua.LNil)
			errorsTable.RawSetInt(i+1, lua.LString(errs[i].Error()))
		} else {
			resultsTable.RawSetInt(i+1, lua.LString(outputs[i]))
			errorsTable.RawSetInt(i+1, lua.LNil)
		}
	}

	L.Push(resultsTable)
	L.Push(errorsTable)
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
		RunID:     rt.rc.Run.ID,
		TraceID:   rt.rc.TraceID,
		OrgID:     &rt.rc.Run.OrgID,
		ThreadID:  &rt.rc.Run.ThreadID,
		UserID:    rt.rc.UserID,
		AgentID:   agentIDFromSkill(rt.rc),
		TimeoutMs: rt.rc.ToolTimeoutMs,
		Budget:    rt.rc.ToolBudget,
		Emitter:   rt.emitter,
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
func (rt *luaRuntime) contextGet(L *lua.LState) int {
	key := L.CheckString(1)
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
		OrgID:   rt.rc.Run.OrgID,
		AgentID: agentIDFromSkill(rt.rc),
	}
	if rt.rc.UserID != nil {
		ident.UserID = *rt.rc.UserID
	}
	return ident
}
