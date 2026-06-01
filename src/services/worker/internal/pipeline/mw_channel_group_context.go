package pipeline

import (
	"context"
	"os"
	"strconv"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
)

const defaultGroupKeepImageTail = 10

// GroupContextTrimDeps 群聊投影与预算裁剪所需的依赖。
type GroupContextTrimDeps struct {
	Pool            CompactPersistDB
	MessagesRepo    data.MessagesRepository
	EventsRepo      CompactRunEventAppender
	EmitDebugEvents bool
	AttachmentStore MessageAttachmentStore
}

// NewChannelGroupContextTrimMiddleware 在 Routing 之后运行，只负责群聊 envelope 投影和图片瘦身。
// 历史压缩统一交给 replacement compact 主路径处理，这里不再直接裁掉消息前缀。
func NewChannelGroupContextTrimMiddleware(deps ...GroupContextTrimDeps) RunMiddleware {
	keepImageTail := defaultGroupKeepImageTail
	cfg := GroupContextTrimDeps{}
	if len(deps) > 0 {
		cfg = deps[0]
	}
	if raw := strings.TrimSpace(os.Getenv("ARKLOOP_CHANNEL_GROUP_KEEP_IMAGE_TAIL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			keepImageTail = n
		}
	}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}

		projectGroupEnvelopes(rc)

		if !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}

		stripOlderImages(rc, keepImageTail)
		messages, err := materializeMessageImages(ctx, cfg.AttachmentStore, rc.Messages)
		if err != nil {
			return err
		}
		rc.Messages = messages
		return next(ctx, rc)
	}
}

// stripOlderImages 将更早的 image part 替换为带 attachment_key 的占位符，仅保留最近 keepImages 个真实图片。
func stripOlderImages(rc *RunContext, keepImages int) {
	if rc == nil || len(rc.Messages) == 0 || keepImages < 0 {
		return
	}
	rewritten, _ := stripOlderImagePartsKeepingTail(rc.Messages, keepImages)
	if len(rewritten) == 0 {
		return
	}
	rc.Messages = rewritten
}

func materializeMessageImages(ctx context.Context, store MessageAttachmentStore, msgs []llm.Message) ([]llm.Message, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		src := out[i].Content
		rebuilt := make([]llm.ContentPart, 0, len(src))
		changed := false
		for j := range src {
			if src[j].Kind() != messagecontent.PartTypeImage || len(src[j].Data) > 0 {
				rebuilt = append(rebuilt, src[j])
				continue
			}
			attachment, dataBytes, ok, err := resolveLazyImage(ctx, store, src[j].Attachment)
			if err != nil {
				return nil, err
			}
			changed = true
			if !ok {
				continue // 图片无法解码，丢弃这张图，保留其余内容
			}
			part := src[j]
			part.Attachment = attachment
			part.Data = dataBytes
			rebuilt = append(rebuilt, part)
		}
		if changed {
			out[i].Content = rebuilt
		}
	}
	return out, nil
}

// IsTelegramGroupLikeConversation 判断 Telegram 侧群 / 超级群 / 频道（非私信）。
func IsTelegramGroupLikeConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}
