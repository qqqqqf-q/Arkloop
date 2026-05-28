package pipeline

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
)

// DeliverArtifactToTelegram 读取 artifact 字节并作为文件附件发送到 Telegram。
// image/* 走 SendPhoto，其余走 SendDocument。账户校验与读取逻辑与 telegram_send_file 工具一致。
func DeliverArtifactToTelegram(
	ctx context.Context,
	store objectstore.Store,
	client *telegrambot.Client,
	token string,
	target ChannelDeliveryTarget,
	accountID uuid.UUID,
	artifactKey string,
) ([]string, error) {
	key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(artifactKey), "artifact:"))
	if key == "" {
		return nil, fmt.Errorf("artifact key is empty")
	}
	if store == nil {
		return nil, fmt.Errorf("artifact storage is not configured")
	}
	if accountID == uuid.Nil || !strings.HasPrefix(key, accountID.String()+"/") {
		return nil, fmt.Errorf("artifact %q is outside the current account", key)
	}
	if client == nil || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("telegram client or token not configured")
	}

	blob, contentType, err := store.GetWithContentType(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read artifact %q: %w", key, err)
	}

	tmpDir, err := os.MkdirTemp("", "arkloop-telegram-artifact-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tmpPath := filepath.Join(tmpDir, artifactTempFilename(key, contentType))
	if err := os.WriteFile(tmpPath, blob, 0o600); err != nil {
		return nil, fmt.Errorf("write temp artifact: %w", err)
	}

	chatID := strings.TrimSpace(target.Conversation.Target)
	threadID := ""
	if target.Conversation.ThreadID != nil {
		threadID = strings.TrimSpace(*target.Conversation.ThreadID)
	}

	var sent *telegrambot.SentMessage
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		sent, err = client.SendPhoto(ctx, token, chatID, tmpPath, "", telegrambot.ParseModeHTML, threadID)
	} else {
		sent, err = client.SendDocument(ctx, token, chatID, tmpPath, "", telegrambot.ParseModeHTML, threadID)
	}
	if err != nil {
		return nil, fmt.Errorf("send artifact to telegram: %w", err)
	}
	if sent == nil {
		return nil, nil
	}
	return []string{strconv.FormatInt(sent.MessageID, 10)}, nil
}

// artifactTempFilename 推导临时文件名：优先用 key 的 basename，否则按 content type 补扩展名。
func artifactTempFilename(key, contentType string) string {
	if name := strings.TrimSpace(filepath.Base(key)); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	ext := strings.TrimSpace(filepath.Ext(key))
	if ext == "" {
		if exts, err := mime.ExtensionsByType(strings.TrimSpace(strings.Split(contentType, ";")[0])); err == nil && len(exts) > 0 {
			ext = exts[0]
		}
	}
	return "artifact" + ext
}
