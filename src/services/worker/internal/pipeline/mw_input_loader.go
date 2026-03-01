package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/jackc/pgx/v5"
)

const threadMessageLimit = 200

// NewInputLoaderMiddleware 加载 run 的 inputJSON 和线程历史消息到 RunContext。
func NewInputLoaderMiddleware(
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		inputJSON, threadMessages, err := loadRunInputs(ctx, rc.Pool, rc.Run, eventsRepo, messagesRepo)
		if err != nil {
			return err
		}

		rc.InputJSON = inputJSON

		llmMessages := make([]llm.Message, 0, len(threadMessages))
		for _, msg := range threadMessages {
			if strings.TrimSpace(msg.Role) == "" {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			parts := []llm.TextPart{}
			if content != "" {
				parts = append(parts, llm.TextPart{Text: content})
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
		"org_id":    run.OrgID.String(),
		"thread_id": run.ThreadID.String(),
	}
	if dataJSON != nil {
		if rawRouteID, ok := dataJSON["route_id"].(string); ok && strings.TrimSpace(rawRouteID) != "" {
			inputJSON["route_id"] = strings.TrimSpace(rawRouteID)
		}
		if rawPersonaID, ok := dataJSON["persona_id"].(string); ok && strings.TrimSpace(rawPersonaID) != "" {
			inputJSON["persona_id"] = strings.TrimSpace(rawPersonaID)
		}
		if rawOutputRouteID, ok := dataJSON["output_route_id"].(string); ok && strings.TrimSpace(rawOutputRouteID) != "" {
			inputJSON["output_route_id"] = strings.TrimSpace(rawOutputRouteID)
		}
	}

	messages, err := messagesRepo.ListByThread(ctx, tx, run.OrgID, run.ThreadID, threadMessageLimit)
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
