local OUTPUT_PERSONA_ID = "search-output"

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

local child, spawn_err = agent.spawn({
  persona_id = OUTPUT_PERSONA_ID,
  context_mode = "isolated",
  input = cot_text,
})
if spawn_err ~= nil then
  error(spawn_err)
end

local resolved, wait_err = agent.wait(child.id)
if wait_err ~= nil then
  error(wait_err)
end

local child_output = resolved.output or ""
if child_output ~= "" then
  context.set_output(child_output)
end
