package pipeline

import (
	"context"

	"arkloop/services/worker/internal/data"
)

// checkSenderIsAdmin 判定渠道消息发送者是否 bot owner。
// 群聊里 SenderUserID 可能是非 owner 的绑定身份,语义是身份相等,
// 不是 membership 角色查询(多用户角色体系已随账户模型塌缩移除)。
func checkSenderIsAdmin(_ context.Context, rc *RunContext) bool {
	if rc.ChannelContext == nil || rc.ChannelContext.SenderUserID == nil {
		return false
	}
	return *rc.ChannelContext.SenderUserID == data.DesktopUserID
}
