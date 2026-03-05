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

local child_output, child_err = agent.run(OUTPUT_PERSONA_ID, cot_text)
if child_err ~= nil then
  error(child_err)
end

if child_output ~= nil and child_output ~= "" then
  context.set_output(child_output)
end
