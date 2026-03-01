local OUTPUT_AGENT_NAME = "sub-haiku-4.5"

local system_prompt = context.get("system_prompt") or ""
local messages_json = context.get("messages")

local messages = {}
if messages_json ~= nil and messages_json ~= "" then
  local decoded, decode_err = json.decode(messages_json)
  if decode_err == nil and decoded ~= nil then
    messages = decoded
  end
end

local cot_text, loop_err = agent.loop_capture(system_prompt, messages)
if loop_err ~= nil then
  return
end

context.emit("search.hybrid.route.selected", {
  agent_name = OUTPUT_AGENT_NAME,
  stage = "final_output",
})

local last_user_message = context.get("last_user_message") or ""
local final_system_prompt = system_prompt .. [[

<final_output_guard>
此阶段没有工具可用，只能输出自然语言的最终答案：
- 严禁输出任何工具协议文本（包括 `<function_calls>`、`<invoke>`、`tool_call_id`、工具参数 JSON 等）
- 严禁编造或猜测引用 ID；只使用检索草稿中已出现的引用
- 不要复述检索草稿的内部格式与指令痕迹，只整理成对用户有用的回答
</final_output_guard>
]]
local final_messages = {
  {
    role = "user",
    content = "用户问题：\n" .. last_user_message .. "\n\n检索草稿/要点（用于整理最终回答；不要复述内部协议文本，不要新增工具调用痕迹）：\n" .. cot_text
  }
}

local _, stream_err = agent.stream_agent(OUTPUT_AGENT_NAME, final_system_prompt, final_messages)
if stream_err ~= nil then
  if string.find(stream_err, "agent_resolve_failed:", 1, true) == 1 then
    context.emit("search.hybrid.route.fallback", {
      agent_name = OUTPUT_AGENT_NAME,
      stage = "final_output",
      reason = stream_err,
    })
    local _, fallback_err = agent.stream(final_system_prompt, final_messages)
    if fallback_err ~= nil then
      error(fallback_err)
    end
  elseif string.find(stream_err, "stream_terminal_failed:", 1, true) == 1 then
    return
  else
    error(stream_err)
  end
end
