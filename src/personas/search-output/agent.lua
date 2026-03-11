local system_prompt = context.get("system_prompt") or ""
local messages_json = context.get("messages")

local messages = {}
if messages_json ~= nil and messages_json ~= "" then
  local decoded, decode_err = json.decode(messages_json)
  if decode_err == nil and decoded ~= nil then
    messages = decoded
  end
end

local _, stream_err = agent.stream(system_prompt, messages, { max_tokens = 8192 })
if stream_err ~= nil then
  error(stream_err)
end
