package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/queue"
	"github.com/google/uuid"
)

const heartbeatMaxConsecutiveErrors = 3

type Handler interface {
	Handle(ctx context.Context, lease queue.JobLease) error
}

type Loop struct {
	queue    queue.JobQueue
	handler  Handler
	locker   RunLocker
	config   Config
	logger   *app.JSONLogger
	notifier *Notifier
}

func NewLoop(
	queueClient queue.JobQueue,
	handler Handler,
	locker RunLocker,
	config Config,
	logger *app.JSONLogger,
	notifier *Notifier,
) (*Loop, error) {
	if queueClient == nil {
		return nil, fmt.Errorf("queue must not be nil")
	}
	if handler == nil {
		return nil, fmt.Errorf("handler must not be nil")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}

	return &Loop{
		queue:    queueClient,
		handler:  handler,
		locker:   locker,
		config:   config,
		logger:   logger,
		notifier: notifier,
	}, nil
}

func (l *Loop) Run(ctx context.Context) error {
	semaphore := make(chan struct{}, l.config.Concurrency)
	errCh := make(chan error, l.config.Concurrency)
	finished := make(chan struct{})

	for worker := 0; worker < l.config.Concurrency; worker++ {
		semaphore <- struct{}{}
		go func() {
			defer func() {
				<-semaphore
			}()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				processed, err := l.RunOnce(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					errCh <- err
					return
				}

				if processed {
					continue
				}

				// 空闲等待：优先 NOTIFY 唤醒，fallback 定时轮询
				l.waitForWork(ctx)
			}
		}()
	}

	go func() {
		for {
			if len(semaphore) == 0 {
				close(finished)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	select {
	case <-ctx.Done():
		<-finished
		return nil
	case err := <-errCh:
		return err
	}
}

func (l *Loop) waitForWork(ctx context.Context) {
	if l.config.PollSeconds <= 0 {
		return
	}
	fallback := time.NewTimer(time.Duration(l.config.PollSeconds * float64(time.Second)))
	defer fallback.Stop()

	if l.notifier != nil {
		wake := l.notifier.Wake()
		select {
		case <-ctx.Done():
		case <-wake:
		case <-fallback.C:
		}
		return
	}

	select {
	case <-ctx.Done():
	case <-fallback.C:
	}
}

func (l *Loop) RunOnce(ctx context.Context) (bool, error) {
	lease, err := l.queue.Lease(ctx, l.config.LeaseSeconds, l.config.QueueJobTypes)
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		l.logger.Error("lease job 失败", app.LogFields{}, map[string]any{"error": err.Error()})
		return false, err
	}
	if lease == nil {
		return false, nil
	}

	l.processLease(ctx, *lease)
	return true, nil
}

func (l *Loop) processLease(ctx context.Context, lease queue.JobLease) {
	fields := fieldsFromLease(lease)

	payloadType := stringValue(lease.PayloadJSON, "type")
	if l.locker != nil && payloadType == queue.RunExecuteJobType {
		runID, ok := extractRunID(lease.PayloadJSON)
		if ok {
			unlock, acquired, err := l.locker.TryAcquire(ctx, runID)
			if err != nil {
				l.logger.Error("acquire advisory lock failed", fields, map[string]any{"error": err.Error()})
				l.safeNack(ctx, lease, nil)
				return
			}
			if !acquired {
				l.logger.Info("run already executing, deferring retry", fields, nil)
				l.safeNack(ctx, lease, nil)
				return
			}
			defer func() {
				if unlock == nil {
					return
				}
				if err := unlock(context.Background()); err != nil {
					l.logger.Error("release advisory lock failed", fields, map[string]any{"error": err.Error()})
				}
			}()
		}
	}

	if !l.heartbeatEnabled() {
		err := l.handler.Handle(ctx, lease)
		l.settleJob(ctx, lease, err)
		return
	}

	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()

	jobDone := make(chan error, 1)
	go func() {
		jobDone <- l.handler.Handle(jobCtx, lease)
	}()

	heartbeatStop := make(chan struct{})
	heartbeatReason := make(chan string, 1)
	go l.heartbeatLoop(jobCtx, lease, heartbeatStop, heartbeatReason)

	select {
	case err := <-jobDone:
		close(heartbeatStop)
		l.settleJob(ctx, lease, err)
	case reason := <-heartbeatReason:
		cancelJob()
		select {
		case <-jobDone:
		case <-time.After(2 * time.Second):
		}
		if reason == "lease_lost" {
			return
		}
		l.logger.Error("heartbeat consecutive failures, stopped current job", fields, map[string]any{"reason": reason})
		l.safeNack(ctx, lease, nil)
	}
}

func (l *Loop) settleJob(ctx context.Context, lease queue.JobLease, runErr error) {
	fields := fieldsFromLease(lease)
	if runErr == nil {
		l.safeAck(ctx, lease)
		return
	}
	if errors.Is(runErr, context.Canceled) {
		l.logger.Info("job cancelled", fields, nil)
		return
	}
	l.logger.Error("job execution failed, will nack for retry", fields, map[string]any{"error": runErr.Error()})
	l.safeNack(ctx, lease, nil)
}

func (l *Loop) heartbeatEnabled() bool {
	if l.config.HeartbeatSeconds <= 0 {
		return false
	}
	if l.config.HeartbeatSeconds >= float64(l.config.LeaseSeconds) {
		l.logger.Info("heartbeat_seconds must be less than lease_seconds, auto-disabled", app.LogFields{}, map[string]any{
			"heartbeat_seconds": l.config.HeartbeatSeconds,
			"lease_seconds":     l.config.LeaseSeconds,
		})
		return false
	}
	return true
}

func (l *Loop) heartbeatLoop(
	ctx context.Context,
	lease queue.JobLease,
	stop <-chan struct{},
	reason chan<- string,
) {
	ticker := time.NewTicker(time.Duration(l.config.HeartbeatSeconds * float64(time.Second)))
	defer ticker.Stop()

	consecutiveErrors := 0
	fields := fieldsFromLease(lease)

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			err := l.queue.Heartbeat(ctx, lease, l.config.LeaseSeconds)
			if err == nil {
				consecutiveErrors = 0
				continue
			}

			var leaseLost *queue.LeaseLostError
			if errors.As(err, &leaseLost) {
				sendReason(reason, "lease_lost")
				return
			}

			consecutiveErrors++
			l.logger.Error("job heartbeat failed", fields, map[string]any{"error": err.Error()})
			if consecutiveErrors >= heartbeatMaxConsecutiveErrors {
				sendReason(reason, "too_many_errors")
				return
			}
		}
	}
}

func sendReason(reason chan<- string, value string) {
	select {
	case reason <- value:
	default:
	}
}

func (l *Loop) safeAck(ctx context.Context, lease queue.JobLease) {
	err := l.queue.Ack(ctx, lease)
	if err == nil {
		return
	}
	var leaseLost *queue.LeaseLostError
	if errors.As(err, &leaseLost) {
		l.logger.Info("ack failed: lease lost", fieldsFromLease(lease), nil)
		return
	}
	l.logger.Error("ack failed", fieldsFromLease(lease), map[string]any{"error": err.Error()})
}

func (l *Loop) safeNack(ctx context.Context, lease queue.JobLease, delay *int) {
	err := l.queue.Nack(ctx, lease, delay)
	if err == nil {
		return
	}
	var leaseLost *queue.LeaseLostError
	if errors.As(err, &leaseLost) {
		l.logger.Info("nack failed: lease lost", fieldsFromLease(lease), nil)
		return
	}
	l.logger.Error("nack failed", fieldsFromLease(lease), map[string]any{"error": err.Error()})
}

func extractRunID(payload map[string]any) (uuid.UUID, bool) {
	raw, ok := payload["run_id"]
	if !ok {
		return uuid.Nil, false
	}
	text, ok := raw.(string)
	if !ok {
		return uuid.Nil, false
	}
	runID, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, false
	}
	return runID, true
}

func fieldsFromLease(lease queue.JobLease) app.LogFields {
	fields := app.LogFields{JobID: stringPtr(lease.JobID.String())}
	if value := stringValue(lease.PayloadJSON, "trace_id"); value != "" {
		fields.TraceID = stringPtr(value)
	}
	if value := stringValue(lease.PayloadJSON, "account_id"); value != "" {
		fields.AccountID = stringPtr(value)
	}
	if value := stringValue(lease.PayloadJSON, "run_id"); value != "" {
		fields.RunID = stringPtr(value)
	}
	return fields
}

func stringValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return text
}

func stringPtr(value string) *string {
	copied := value
	return &copied
}
