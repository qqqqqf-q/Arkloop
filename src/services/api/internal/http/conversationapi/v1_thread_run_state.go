package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/threadrunstate"

	"github.com/google/uuid"
)

type threadRunStateEvent struct {
	Type        string  `json:"type"`
	ThreadID    string  `json:"thread_id"`
	ActiveRunID *string `json:"active_run_id"`
	Title       *string `json:"title"`
}

func streamThreadRunStateEvents(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	runRepo *data.RunEventRepository,
	auditWriter *audit.Writer,
	sseConfig SSEConfig,
	apiKeysRepo *data.APIKeysRepository,
	bus eventbus.EventBus,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}

		traceID := observability.TraceIDFromContext(r.Context())
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		if threadRepo == nil || runRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}

		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataThreadsRead, w, traceID) {
			return
		}

		heartbeatDuration := time.Duration(float64(time.Second) * sseConfig.HeartbeatSeconds)
		if heartbeatDuration <= 0 {
			heartbeatDuration = 15 * time.Second
		}
		flusher, canFlush := w.(nethttp.Flusher)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(nethttp.StatusOK)

		if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
			return
		}
		if canFlush {
			flusher.Flush()
		}
		renewSSEWriteDeadline(w, heartbeatDuration)

		payloadCh := make(chan string, 8)
		subscribeThreadRunStateEvents(r.Context(), payloadCh, bus)

		ticker := time.NewTicker(heartbeatDuration)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case payload, ok := <-payloadCh:
				if !ok {
					return
				}
				if err := writeThreadRunStateEvent(r.Context(), w, actor, threadRepo, projectRepo, teamRepo, runRepo, payload); err != nil {
					return
				}
				if canFlush {
					flusher.Flush()
				}
				renewSSEWriteDeadline(w, heartbeatDuration)
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				if canFlush {
					flusher.Flush()
				}
				renewSSEWriteDeadline(w, heartbeatDuration)
			}
		}
	}
}

func subscribeThreadRunStateEvents(
	ctx context.Context,
	payloadCh chan<- string,
	bus eventbus.EventBus,
) {
	send := func(payload string) {
		if strings.TrimSpace(payload) == "" {
			return
		}
		select {
		case payloadCh <- payload:
		default:
		}
	}

	if bus != nil {
		sub, err := bus.Subscribe(ctx, threadrunstate.Topic)
		if err == nil {
			go func() {
				defer func() { _ = sub.Close() }()
				for {
					select {
					case <-ctx.Done():
						return
					case msg, ok := <-sub.Channel():
						if !ok {
							return
						}
						send(msg.Payload)
					}
				}
			}()
		}
	}
}

func writeThreadRunStateEvent(
	ctx context.Context,
	w nethttp.ResponseWriter,
	actor *httpkit.Actor,
	threadRepo *data.ThreadRepository,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
	runRepo *data.RunEventRepository,
	payload string,
) error {
	accountID, threadID, ok := threadrunstate.Decode(payload)
	if !ok || actor == nil || accountID != actor.AccountID {
		return nil
	}

	thread, err := threadRepo.GetByID(ctx, threadID)
	if err != nil {
		return err
	}
	if thread == nil {
		return nil
	}
	allowed, err := canStreamThreadRunState(ctx, actor, thread, projectRepo, teamRepo)
	if err != nil || !allowed {
		return err
	}

	activeRunID, err := runRepo.GetActiveRunIDForThread(ctx, actor.AccountID, threadID)
	if err != nil {
		return err
	}
	var active *string
	if activeRunID != nil && *activeRunID != uuid.Nil {
		value := activeRunID.String()
		active = &value
	}
	encoded, err := json.Marshal(threadRunStateEvent{
		Type:        "thread.run_state",
		ThreadID:    threadID.String(),
		ActiveRunID: active,
		Title:       thread.Title,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: thread.run_state\ndata: %s\n\n", encoded)
	return err
}

func canStreamThreadRunState(
	ctx context.Context,
	actor *httpkit.Actor,
	thread *data.Thread,
	projectRepo *data.ProjectRepository,
	teamRepo *data.TeamRepository,
) (bool, error) {
	if actor == nil || thread == nil || actor.AccountID != thread.AccountID {
		return false, nil
	}
	if thread.CreatedByUserID != nil && *thread.CreatedByUserID == actor.UserID {
		return true, nil
	}
	if thread.ProjectID == nil || projectRepo == nil {
		return false, nil
	}
	project, err := projectRepo.GetByID(ctx, *thread.ProjectID)
	if err != nil || project == nil {
		return false, err
	}
	switch project.Visibility {
	case "org":
		return true, nil
	case "team":
		if project.TeamID == nil || teamRepo == nil {
			return false, nil
		}
		return teamRepo.IsMember(ctx, *project.TeamID, actor.UserID)
	default:
		return false, nil
	}
}
