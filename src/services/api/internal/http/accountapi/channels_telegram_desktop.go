//go:build desktop

package accountapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/api/internal/observability"
	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
)

const telegramLongPollSeconds = 50

var logTelegramPollNoActiveChannels sync.Once

// TelegramDesktopPollerDeps 桌面模式 getUpdates 长轮询依赖。
type TelegramDesktopPollerDeps struct {
	ChannelsRepo            *data.ChannelsRepository
	ChannelIdentitiesRepo   *data.ChannelIdentitiesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
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
	EntitlementService      *entitlement.Service
	TelegramBotClient       *telegrambot.Client
	MessageAttachmentStore  MessageAttachmentPutStore
	PollInterval            time.Duration
	PollLimit               int
	// TelegramMode 为 webhook 时不启动桌面轮询；空视为 polling。
	TelegramMode string
	Bus          eventbus.EventBus
}

// StartTelegramDesktopPoller 启动 Telegram 长轮询；与 API / Worker 通过 TryAcquireTelegramDesktopPollLeader 互斥。
func StartTelegramDesktopPoller(ctx context.Context, deps TelegramDesktopPollerDeps) {
	var abort string
	switch {
	case ctx == nil:
		abort = "nil_ctx"
	case deps.ChannelsRepo == nil:
		abort = "nil_channels_repo"
	case deps.ChannelIdentitiesRepo == nil:
		abort = "nil_channel_identities_repo"
	case deps.ChannelIdentityLinksRepo == nil:
		abort = "nil_channel_identity_links_repo"
	case deps.ChannelBindCodesRepo == nil:
		abort = "nil_channel_bind_codes_repo"
	case deps.ChannelDMThreadsRepo == nil:
		abort = "nil_channel_dm_threads_repo"
	case deps.ChannelGroupThreadsRepo == nil:
		abort = "nil_channel_group_threads_repo"
	case deps.ChannelReceiptsRepo == nil:
		abort = "nil_channel_receipts_repo"
	case deps.SecretsRepo == nil:
		abort = "nil_secrets_repo"
	case deps.PersonasRepo == nil:
		abort = "nil_personas_repo"
	case deps.UsersRepo == nil:
		abort = "nil_users_repo"
	case deps.AccountRepo == nil:
		abort = "nil_account_repo"
	case deps.AccountMembershipRepo == nil:
		abort = "nil_membership_repo"
	case deps.ProjectRepo == nil:
		abort = "nil_project_repo"
	case deps.ThreadRepo == nil:
		abort = "nil_thread_repo"
	case deps.MessageRepo == nil:
		abort = "nil_message_repo"
	case deps.RunEventRepo == nil:
		abort = "nil_run_event_repo"
	case deps.JobRepo == nil:
		abort = "nil_job_repo"
	case deps.CreditsRepo == nil:
		abort = "nil_credits_repo"
	case deps.Pool == nil:
		abort = "nil_pool"
	}
	if abort != "" {
		slog.Warn("telegram_desktop_poll_abort", "reason", abort)
		return
	}

	if !shareddesktop.TryAcquireTelegramDesktopPollLeader() {
		slog.Info("telegram_desktop_poll_skip", "reason", "leader_held")
		return
	}
	if telegramModeUsesWebhook(deps.TelegramMode) {
		slog.Info("telegram_desktop_poll_skip", "reason", "webhook_mode", "mode", strings.TrimSpace(deps.TelegramMode))
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
			slog.Warn("telegram_desktop_poll_abort", "reason", "ledger_repo", "err", err.Error())
			return
		}
	}

	telegramForConnector := deps.TelegramBotClient
	if telegramForConnector == nil {
		telegramForConnector = telegrambot.NewClient("", nil)
	}

	var busInputNotify func(ctx context.Context, runID uuid.UUID)
	if deps.Bus != nil {
		bus := deps.Bus
		busInputNotify = func(ctx context.Context, runID uuid.UUID) {
			_ = bus.Publish(ctx, fmt.Sprintf("run_events:%s", runID.String()), "")
		}
	}

	connector := telegramConnector{
		channelsRepo:            deps.ChannelsRepo,
		channelIdentitiesRepo:   deps.ChannelIdentitiesRepo,
		channelIdentityLinksRepo: deps.ChannelIdentityLinksRepo,
		channelBindCodesRepo:    deps.ChannelBindCodesRepo,
		channelDMThreadsRepo:    deps.ChannelDMThreadsRepo,
		channelGroupThreadsRepo: deps.ChannelGroupThreadsRepo,
		channelReceiptsRepo:     deps.ChannelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		scheduledTriggersRepo:   &data.ScheduledTriggersRepository{},
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
		inputNotify:             busInputNotify,
		bus:                     deps.Bus,
	}

	pollHTTP := &http.Client{Timeout: time.Duration(telegramLongPollSeconds+15) * time.Second}
	pollClient := telegrambot.NewClient(strings.TrimSpace(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL")), pollHTTP)

	go func() {
		slog.Info("telegram_desktop_poll_started", "poll_limit", limit, "long_poll_sec", telegramLongPollSeconds)
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
		slog.Warn("telegram_poll_list_channels", "err", err.Error())
		return err
	}
	if len(channels) == 0 {
		logTelegramPollNoActiveChannels.Do(func() {
			slog.Info("telegram_poll_tick", "active_channels", 0)
		})
	}
	var lastErr error
	for _, ch := range channels {
		// 注意：Create 时总会写入指向本机 API 的 webhook 路径（验签/路由），不代表 Telegram 侧已 setWebhook。
		// 桌面 polling 依赖 getUpdates，不能按 DB 里的 webhook_url 跳过，否则所有频道都会被跳过。
		token, err := secretsRepo.DecryptByID(ctx, derefUUID(ch.CredentialsID))
		if err != nil {
			slog.Warn("telegram_poll_token", "channel_id", ch.ID.String(), "err", err.Error())
			lastErr = err
			continue
		}
		if token == nil || strings.TrimSpace(*token) == "" {
			slog.Warn("telegram_poll_token", "channel_id", ch.ID.String(), "err", "empty")
			continue
		}

		req := telegrambot.GetUpdatesRequest{
			Limit:          limit,
			Updates:        []string{"message", "edited_message"},
			TimeoutSeconds: longPollSec,
		}
		if offset, ok := offsets[ch.ID]; ok && offset > 0 {
			req.Offset = &offset
		}

		var updates []telegramUpdate
		err = client.GetUpdates(ctx, strings.TrimSpace(*token), req, &updates)
		if err != nil {
			slog.Warn("telegram_poll_get_updates", "channel_id", ch.ID.String(), "err", err.Error())
			lastErr = err
			continue
		}

		nextOffset := offsets[ch.ID]
		for _, update := range updates {
			err := connector.HandleUpdateForPoll(ctx, observability.NewTraceID(), ch, strings.TrimSpace(*token), update)
			if err != nil {
				if !errors.Is(err, conversationapi.ErrUnsupportedAttachmentType) {
					slog.Warn("telegram_poll_handle_update", "channel_id", ch.ID.String(), "err", err.Error())
					lastErr = err
					break
				}
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
