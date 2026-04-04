//go:build desktop

package accountapi

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/pgnotify"

	"github.com/google/uuid"
)

// QQOneBotWSListenerDeps 桌面模式 OneBot WS 客户端依赖。
type QQOneBotWSListenerDeps struct {
	ChannelsRepo             *data.ChannelsRepository
	ChannelIdentitiesRepo    *data.ChannelIdentitiesRepository
	ChannelBindCodesRepo     *data.ChannelBindCodesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ChannelDMThreadsRepo     *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo  *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo      *data.ChannelMessageReceiptsRepository
	PersonasRepo             *data.PersonasRepository
	ThreadRepo               *data.ThreadRepository
	MessageRepo              *data.MessageRepository
	RunEventRepo             *data.RunEventRepository
	JobRepo                  *data.JobRepository
	Pool                     data.DB
}

// StartQQOneBotWSListener 启动 QQ OneBot WS Client Listener（桌面模式）。
func StartQQOneBotWSListener(ctx context.Context, deps QQOneBotWSListenerDeps) {
	if deps.ChannelsRepo == nil || deps.Pool == nil {
		return
	}

	var channelLedgerRepo *data.ChannelMessageLedgerRepository
	repo, err := data.NewChannelMessageLedgerRepository(deps.Pool)
	if err != nil {
		slog.Warn("qq_ws_listener_abort", "reason", "ledger_repo", "err", err)
		return
	}
	channelLedgerRepo = repo

	connector := &qqConnector{
		channelsRepo:             deps.ChannelsRepo,
		channelIdentitiesRepo:    deps.ChannelIdentitiesRepo,
		channelBindCodesRepo:     deps.ChannelBindCodesRepo,
		channelIdentityLinksRepo: deps.ChannelIdentityLinksRepo,
		channelDMThreadsRepo:     deps.ChannelDMThreadsRepo,
		channelGroupThreadsRepo:  deps.ChannelGroupThreadsRepo,
		channelReceiptsRepo:      deps.ChannelReceiptsRepo,
		channelLedgerRepo:        channelLedgerRepo,
		personasRepo:             deps.PersonasRepo,
		threadRepo:               deps.ThreadRepo,
		messageRepo:              deps.MessageRepo,
		runEventRepo:             deps.RunEventRepo,
		jobRepo:                  deps.JobRepo,
		pool:                     deps.Pool,
		inputNotify: func(ctx context.Context, runID uuid.UUID) {
			if _, err := deps.Pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("qq_ws_active_run_notify_failed", "run_id", runID, "error", err)
			}
		},
	}

	go qqWSListenerLoop(ctx, deps.ChannelsRepo, connector)
}

func qqWSListenerLoop(ctx context.Context, channelsRepo *data.ChannelsRepository, connector *qqConnector) {
	slog.Info("qq_ws_listener_started")

	var activeListeners sync.Map // uuid.UUID -> *onebotclient.WSListener

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	check := func() {
		channels, err := channelsRepo.ListActiveByType(ctx, "qq")
		if err != nil {
			slog.Warn("qq_ws_listener_list_error", "error", err)
			return
		}

		activeIDs := make(map[uuid.UUID]bool, len(channels))
		for _, ch := range channels {
			activeIDs[ch.ID] = true

			cfg, err := resolveQQChannelConfig(ch.ConfigJSON)
			if err != nil {
				continue
			}
			wsURL := strings.TrimSpace(cfg.OneBotWSURL)
			if wsURL == "" {
				continue
			}
			if _, exists := activeListeners.Load(ch.ID); exists {
				continue
			}

			chCopy := ch
			token := cfg.OneBotToken
			if token == "" {
				if mgr := getNapCatManagerIfExists(); mgr != nil {
					_, token = mgr.WSEndpoint()
				}
			}
			listener := onebotclient.NewWSListener(wsURL, token, func(evCtx context.Context, event onebotclient.Event) {
				traceID := observability.NewTraceID()
				if err := connector.HandleEvent(evCtx, traceID, chCopy, event); err != nil {
					slog.Warn("qq_ws_event_error", "channel_id", chCopy.ID, "error", err)
				}
			}, nil)

			activeListeners.Store(ch.ID, listener)
			listener.Start(ctx)
			slog.Info("qq_ws_listener_channel_started", "channel_id", ch.ID, "ws_url", wsURL)
		}

		activeListeners.Range(func(key, value any) bool {
			id := key.(uuid.UUID)
			if !activeIDs[id] {
				if l, ok := value.(*onebotclient.WSListener); ok {
					l.Stop()
				}
				activeListeners.Delete(id)
				slog.Info("qq_ws_listener_channel_stopped", "channel_id", id)
			}
			return true
		})
	}

	check()
	for {
		select {
		case <-ctx.Done():
			activeListeners.Range(func(_, value any) bool {
				if l, ok := value.(*onebotclient.WSListener); ok {
					l.Stop()
				}
				return true
			})
			return
		case <-ticker.C:
			check()
		}
	}
}
