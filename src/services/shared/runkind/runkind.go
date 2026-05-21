package runkind

const Heartbeat = "heartbeat"
const Discuss = "discuss"
const Impression = "impression"
const Suggestion = "suggestion"
const ActivityRecorder = "activity_recorder"
const StickerRegister = "sticker_register"
const SubagentCallback = "subagent_callback"
const ScheduledJob = "scheduled_job"

// DefaultHeartbeatIntervalMinutes 是未配置时的默认心跳间隔。
const DefaultHeartbeatIntervalMinutes = 30

// DefaultPersonaKey 是 scheduled job 在 job.PersonaKey 为空且无法从 thread 推断时的兜底 persona。
const DefaultPersonaKey = "normal"
