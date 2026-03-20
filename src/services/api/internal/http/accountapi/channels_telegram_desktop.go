//go:build desktop

package accountapi

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"
	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
)

const telegramLongPollSeconds = 50

// TelegramDesktopPollerDeps 桌面模式 getUpdates 长轮询依赖。
type TelegramDesktopPollerDeps struct {
	ChannelsRepo            *data.ChannelsRepository
	ChannelIdentitiesRepo   *data.ChannelIdentitiesRepository
	ChannelBindCodesRepo    *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo    *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	ChannelLedgerRepo       *data.ChannelMessageLedgerRepository
	SecretsRepo             *data.SecretsRepository
	PersonasRepo            *data.PersonasRepository
	UsersRepo               *data.UserRepository
	AccountRepo             *data.AccountRepository
	AccountMembershipRepo   *data.AccountMembershipRepository
	ProjectRepo             *data.ProjectRepository
	ThreadRepo              *data.ThreadRepository
	MessageRepo             *data.MessageRepository
	RunEventRepo            *data.RunEventRepository
	JobRepo                 *data.JobRepository
	CreditsRepo             *data.CreditsRepository
	Pool                    data.DB
	EntitlementService       *entitlement.Service
	TelegramBotClient        *telegrambot.Client
	MessageAttachmentStore   MessageAttachmentPutStore
	PollInterval             time.Duration
	PollLimit                int
	// TelegramMode 为 webhook 时不启动桌面轮询；空视为 polling。
	TelegramMode string
}

// StartTelegramDesktopPoller 启动 Telegram 长轮询；与 API / Worker 通过 TryAcquireTelegramDesktopPollLeader 互斥。
func StartTelegramDesktopPoller(ctx context.Context, deps TelegramDesktopPollerDeps) {
	if ctx == nil ||
		deps.ChannelsRepo == nil ||
		deps.ChannelIdentitiesRepo == nil ||
		deps.ChannelBindCodesRepo == nil ||
		deps.ChannelDMThreadsRepo == nil ||
		deps.ChannelGroupThreadsRepo == nil ||
		deps.ChannelReceiptsRepo == nil ||
		deps.SecretsRepo == nil ||
		deps.PersonasRepo == nil ||
		deps.UsersRepo == nil ||
		deps.AccountRepo == nil ||
		deps.AccountMembershipRepo == nil ||
		deps.ProjectRepo == nil ||
		deps.ThreadRepo == nil ||
		deps.MessageRepo == nil ||
		deps.RunEventRepo == nil ||
		deps.JobRepo == nil ||
		deps.CreditsRepo == nil ||
		deps.Pool == nil {
		return
	}

	if !shareddesktop.TryAcquireTelegramDesktopPollLeader() {
		return
	}
	if telegramModeUsesWebhook(deps.TelegramMode) {
		return
	}

	limit := deps.PollLimit
	if limit <= 0 {
		limit = 20
	}
	channelLedgerRepo := deps.ChannelLedgerRepo
	if channelLedgerRepo == nil {
		var err error
		channelLedgerRepo, err = data.NewChannelMessageLedgerRepository(deps.Pool)
		if err != nil {
			return
		}
	}

	telegramForConnector := deps.TelegramBotClient
	if telegramForConnector == nil {
		telegramForConnector = telegrambot.NewClient("", nil)
	}

	connector := telegramConnector{
		channelsRepo:            deps.ChannelsRepo,
		channelIdentitiesRepo:   deps.ChannelIdentitiesRepo,
		channelBindCodesRepo:    deps.ChannelBindCodesRepo,
		channelDMThreadsRepo:    deps.ChannelDMThreadsRepo,
		channelGroupThreadsRepo: deps.ChannelGroupThreadsRepo,
		channelReceiptsRepo:     deps.ChannelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		personasRepo:            deps.PersonasRepo,
		usersRepo:               deps.UsersRepo,
		accountRepo:             deps.AccountRepo,
		membershipRepo:          deps.AccountMembershipRepo,
		projectRepo:             deps.ProjectRepo,
		threadRepo:              deps.ThreadRepo,
		messageRepo:             deps.MessageRepo,
		runEventRepo:            deps.RunEventRepo,
		jobRepo:                 deps.JobRepo,
		creditsRepo:             deps.CreditsRepo,
		pool:                    deps.Pool,
		entitlementSvc:          deps.EntitlementService,
		telegramClient:          telegramForConnector,
		attachmentStore:         deps.MessageAttachmentStore,
	}

	pollHTTP := &http.Client{Timeout: time.Duration(telegramLongPollSeconds+15) * time.Second}
	pollClient := telegrambot.NewClient(strings.TrimSpace(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL")), pollHTTP)

	go func() {
		offsets := make(map[uuid.UUID]int64)
		backoff := time.Duration(0)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if backoff > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = 0
			}
			err := pollTelegramDesktopOnce(ctx, pollClient, connector, deps.ChannelsRepo, deps.SecretsRepo, offsets, limit, telegramLongPollSeconds)
			if err != nil {
				backoff = 2 * time.Second
			}
		}
	}()
}

func pollTelegramDesktopOnce(
	ctx context.Context,
	client *telegrambot.Client,
	connector telegramConnector,
	channelsRepo *data.ChannelsRepository,
	secretsRepo *data.SecretsRepository,
	offsets map[uuid.UUID]int64,
	limit int,
	longPollSec int,
) error {
	channels, err := channelsRepo.ListActiveByType(ctx, "telegram")
	if err != nil {
		return err
	}
	var lastErr error
	for _, ch := range channels {
		// 注意：Create 时总会写入指向本机 API 的 webhook 路径（验签/路由），不代表 Telegram 侧已 setWebhook。
		// 桌面 polling 依赖 getUpdates，不能按 DB 里的 webhook_url 跳过，否则所有频道都会被跳过。
		token, err := secretsRepo.DecryptByID(ctx, derefUUID(ch.CredentialsID))
		if err != nil || token == nil || strings.TrimSpace(*token) == "" {
			continue
		}

		req := telegrambot.GetUpdatesRequest{
			Limit:          limit,
			Updates:        []string{"message"},
			TimeoutSeconds: longPollSec,
		}
		if offset, ok := offsets[ch.ID]; ok && offset > 0 {
			req.Offset = &offset
		}

		var updates []telegramUpdate
		err = client.GetUpdates(ctx, strings.TrimSpace(*token), req, &updates)
		if err != nil {
			lastErr = err
			continue
		}

		nextOffset := offsets[ch.ID]
		for _, update := range updates {
			if err := connector.HandleUpdate(ctx, observability.NewTraceID(), ch, strings.TrimSpace(*token), update); err != nil {
				lastErr = err
				break
			}
			if candidate := update.UpdateID + 1; candidate > nextOffset {
				nextOffset = candidate
			}
		}
		if nextOffset > 0 {
			offsets[ch.ID] = nextOffset
		}
	}
	return lastErr
}
