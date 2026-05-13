//go:build desktop

package accountapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/weixinclient"

	"github.com/google/uuid"
)

const weixinDefaultBaseURL = "https://ilinkai.weixin.qq.com"

// WeChatPollingDeps 微信长轮询依赖。
type WeChatPollingDeps struct {
	ChannelsRepo            *data.ChannelsRepository
	ChannelIdentitiesRepo   *data.ChannelIdentitiesRepository
	ChannelDMThreadsRepo    *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo     *data.ChannelMessageReceiptsRepository
	PersonasRepo            *data.PersonasRepository
	ThreadRepo              *data.ThreadRepository
	MessageRepo             *data.MessageRepository
	RunEventRepo            *data.RunEventRepository
	JobRepo                 *data.JobRepository
	SecretsRepo             *data.SecretsRepository
	Pool                    data.DB
	Bus                     eventbus.EventBus
}

// StartWeChatPollingListener 启动微信长轮询消息监听。
func StartWeChatPollingListener(ctx context.Context, deps WeChatPollingDeps) {
	if deps.ChannelsRepo == nil || deps.Pool == nil {
		return
	}

	var channelLedgerRepo *data.ChannelMessageLedgerRepository
	repo, err := data.NewChannelMessageLedgerRepository(deps.Pool)
	if err != nil {
		slog.Warn("weixin_poll_abort", "reason", "ledger_repo", "err", err)
		return
	}
	channelLedgerRepo = repo

	connector := &weixinConnector{
		channelsRepo:            deps.ChannelsRepo,
		channelIdentitiesRepo:   deps.ChannelIdentitiesRepo,
		channelDMThreadsRepo:    deps.ChannelDMThreadsRepo,
		channelGroupThreadsRepo: deps.ChannelGroupThreadsRepo,
		channelReceiptsRepo:     deps.ChannelReceiptsRepo,
		channelLedgerRepo:       channelLedgerRepo,
		personasRepo:            deps.PersonasRepo,
		threadRepo:              deps.ThreadRepo,
		messageRepo:             deps.MessageRepo,
		runEventRepo:            deps.RunEventRepo,
		jobRepo:                 deps.JobRepo,
		pool:                    deps.Pool,
		inputNotify: func(ctx context.Context, runID uuid.UUID) {
			if deps.Bus != nil {
				_ = deps.Bus.Publish(ctx, fmt.Sprintf("run_events:%s", runID.String()), "")
			} else {
				if _, err := deps.Pool.Exec(ctx, "SELECT pg_notify('run_input', $1)", runID.String()); err != nil {
					slog.Warn("weixin_active_run_notify_failed", "run_id", runID, "error", err)
				}
			}
		},
	}

	go weixinPollingLoop(ctx, deps.ChannelsRepo, deps.SecretsRepo, connector)
}

func weixinPollingLoop(
	ctx context.Context,
	channelsRepo *data.ChannelsRepository,
	secretsRepo *data.SecretsRepository,
	connector *weixinConnector,
) {
	slog.Info("weixin_poll_started")

	var activePolls sync.Map // uuid.UUID -> context.CancelFunc

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	check := func() {
		channels, err := channelsRepo.ListActiveByType(ctx, "weixin")
		if err != nil {
			slog.Warn("weixin_poll_list_error", "error", err)
			return
		}

		activeIDs := make(map[uuid.UUID]bool, len(channels))
		for _, ch := range channels {
			activeIDs[ch.ID] = true
			if _, exists := activePolls.Load(ch.ID); exists {
				continue
			}

			if ch.CredentialsID == nil {
				slog.Warn("weixin_poll_skip_no_credentials", "channel_id", ch.ID)
				continue
			}
			token, err := secretsRepo.DecryptByID(ctx, *ch.CredentialsID)
			if err != nil || token == nil || strings.TrimSpace(*token) == "" {
				slog.Warn("weixin_poll_skip_bad_credentials", "channel_id", ch.ID, "error", err)
				continue
			}

			cfg, _ := resolveWeixinChannelConfig(ch.ConfigJSON)
			baseURL := strings.TrimSpace(cfg.BaseURL)
			if baseURL == "" {
				baseURL = weixinDefaultBaseURL
			}

			pollCtx, cancel := context.WithCancel(ctx)
			chCopy := ch
			go startWeixinLongPoll(pollCtx, chCopy, baseURL, *token, connector, cancel)
			activePolls.Store(ch.ID, cancel)
			slog.Info("weixin_poll_channel_started", "channel_id", ch.ID, "base_url", baseURL)
		}

		activePolls.Range(func(key, value any) bool {
			id := key.(uuid.UUID)
			if !activeIDs[id] {
				if cancel, ok := value.(context.CancelFunc); ok {
					cancel()
				}
				activePolls.Delete(id)
				slog.Info("weixin_poll_channel_stopped", "channel_id", id)
			}
			return true
		})
	}

	check()
	for {
		select {
		case <-ctx.Done():
			activePolls.Range(func(_, value any) bool {
				if cancel, ok := value.(context.CancelFunc); ok {
					cancel()
				}
				return true
			})
			return
		case <-ticker.C:
			check()
		}
	}
}

func startWeixinLongPoll(
	ctx context.Context,
	ch data.Channel,
	baseURL, token string,
	connector *weixinConnector,
	cancel context.CancelFunc,
) {
	defer cancel()

	httpClient := &http.Client{Timeout: 45 * time.Second}
	client := weixinclient.NewClient(baseURL, token, httpClient)
	connector.weixinClient = client

	var cursor string
	backoff := time.Duration(0)
	const maxBackoff = 30 * time.Second

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
		}

		resp, err := client.GetUpdates(ctx, cursor)
		if err != nil {
			if backoff == 0 {
				backoff = 1 * time.Second
			} else {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			slog.Warn("weixin_poll_get_updates_error", "channel_id", ch.ID, "error", err, "backoff", backoff)
			continue
		}

		backoff = 0
		cursor = resp.GetUpdatesBuf

		for _, msg := range resp.Msgs {
			msgCopy := msg
			traceID := observability.NewTraceID()
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("weixin_poll_handle_panic", "channel_id", ch.ID, "panic", r)
					}
				}()
				if err := connector.HandleWeChatMessage(ctx, traceID, ch, msgCopy); err != nil {
					slog.Warn("weixin_poll_handle_error", "channel_id", ch.ID, "error", err)
				}
			}()
		}
	}
}
