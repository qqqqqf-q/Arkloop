package pipeline

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/jackc/pgx/v5"
)

type MessageAttachmentStore interface {
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

// NewInputLoaderMiddleware 加载 run 的 inputJSON 和线程历史消息到 RunContext。
func NewInputLoaderMiddleware(
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		messageLimit := rc.ThreadMessageHistoryLimit
		if messageLimit <= 0 {
			messageLimit = 200
		}
		inputJSON, threadMessages, err := loadRunInputs(ctx, rc.Pool, rc.Run, eventsRepo, messagesRepo, messageLimit)
		if err != nil {
			return err
		}

		rc.InputJSON = inputJSON

		llmMessages := make([]llm.Message, 0, len(threadMessages))
		for _, msg := range threadMessages {
			if strings.TrimSpace(msg.Role) == "" {
				continue
			}
			parts, err := buildMessageParts(ctx, attachmentStore, msg)
			if err != nil {
				return err
			}
			llmMessages = append(llmMessages, llm.Message{
				Role:    msg.Role,
				Content: parts,
			})
		}
		rc.Messages = llmMessages

		return next(ctx, rc)
	}
}

func loadRunInputs(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	messageLimit int,
) (map[string]any, []data.ThreadMessage, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	_, dataJSON, err := eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, nil, err
	}

	inputJSON := map[string]any{
		"account_id":    run.AccountID.String(),
		"thread_id": run.ThreadID.String(),
	}
	if dataJSON != nil {
		if rawRouteID, ok := dataJSON["route_id"].(string); ok && strings.TrimSpace(rawRouteID) != "" {
			inputJSON["route_id"] = strings.TrimSpace(rawRouteID)
		}
		if rawPersonaID, ok := dataJSON["persona_id"].(string); ok && strings.TrimSpace(rawPersonaID) != "" {
			inputJSON["persona_id"] = strings.TrimSpace(rawPersonaID)
		}
		if rawRole, ok := dataJSON["role"].(string); ok && strings.TrimSpace(rawRole) != "" {
			inputJSON["role"] = strings.TrimSpace(rawRole)
		}
		if rawOutputRouteID, ok := dataJSON["output_route_id"].(string); ok && strings.TrimSpace(rawOutputRouteID) != "" {
			inputJSON["output_route_id"] = strings.TrimSpace(rawOutputRouteID)
		}
	}

	messages, err := messagesRepo.ListByThread(ctx, tx, run.AccountID, run.ThreadID, messageLimit)
	if err != nil {
		return nil, nil, err
	}

	// 提取最后一条用户消息，供 Lua 脚本通过 context.get("last_user_message") 访问
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			inputJSON["last_user_message"] = strings.TrimSpace(messages[i].Content)
			break
		}
	}

	return inputJSON, messages, nil
}

func buildMessageParts(ctx context.Context, store MessageAttachmentStore, msg data.ThreadMessage) ([]llm.ContentPart, error) {
	if len(msg.ContentJSON) == 0 {
		return fallbackTextParts(msg.Content), nil
	}
	parsed, err := messagecontent.Parse(msg.ContentJSON)
	if err != nil {
		return fallbackTextParts(msg.Content), nil
	}
	content, err := messagecontent.Normalize(parsed.Parts)
	if err != nil {
		return fallbackTextParts(msg.Content), nil
	}
	parts := make([]llm.ContentPart, 0, len(content.Parts))
	for _, part := range content.Parts {
		switch part.Type {
		case messagecontent.PartTypeText:
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, llm.ContentPart{Type: messagecontent.PartTypeText, Text: part.Text})
		case messagecontent.PartTypeFile:
			parts = append(parts, llm.ContentPart{
				Type:          messagecontent.PartTypeFile,
				Attachment:    part.Attachment,
				ExtractedText: part.ExtractedText,
			})
		case messagecontent.PartTypeImage:
			if part.Attachment == nil {
				return nil, fmt.Errorf("message image attachment is required")
			}
			if store == nil {
				return nil, fmt.Errorf("message attachment store not configured")
			}
			dataBytes, contentType, err := store.GetWithContentType(ctx, part.Attachment.Key)
			if err != nil {
				if objectstore.IsNotFound(err) {
					return nil, fmt.Errorf("message attachment not found")
				}
				return nil, err
			}
			attachment := *part.Attachment
			if strings.TrimSpace(contentType) != "" {
				attachment.MimeType = strings.TrimSpace(contentType)
			}
			parts = append(parts, llm.ContentPart{
				Type:       messagecontent.PartTypeImage,
				Attachment: &attachment,
				Data:       dataBytes,
			})
		}
	}
	if len(parts) == 0 {
		return fallbackTextParts(msg.Content), nil
	}
	return parts, nil
}

func fallbackTextParts(content string) []llm.ContentPart {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: content}}
}
