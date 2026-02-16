package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"arkloop/services/worker_go/internal/app"
	"arkloop/services/worker_go/internal/queue"
	"github.com/google/uuid"
)

const heartbeatMaxConsecutiveErrors = 3

type Handler interface {
	Handle(ctx context.Context, lease queue.JobLease) error
}

type Loop struct {
	queue   queue.JobQueue
	handler Handler
	locker  RunLocker
	config  Config
	logger  *app.JSONLogger
}

func NewLoop(
	queueClient queue.JobQueue,
	handler Handler,
	locker RunLocker,
	config Config,
	logger *app.JSONLogger,
) (*Loop, error) {
	if queueClient == nil {
		return nil, fmt.Errorf("queue 不能为空")
	}
	if handler == nil {
		return nil, fmt.Errorf("handler 不能为空")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = app.NewJSONLogger("worker_go", nil)
	}

	return &Loop{
		queue:   queueClient,
		handler: handler,
		locker:  locker,
		config:  config,
		logger:  logger,
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

				_, err := l.RunOnce(ctx)
				if err != nil {
					errCh <- err
					return
				}

				if l.config.PollSeconds <= 0 {
					continue
				}
				wait := time.Duration(l.config.PollSeconds * float64(time.Second))
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
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

func (l *Loop) RunOnce(ctx context.Context) (bool, error) {
	lease, err := l.queue.Lease(ctx, l.config.LeaseSeconds, l.config.QueueJobTypes)
	if err != nil {
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
				l.logger.Error("acquire advisory lock 失败", fields, map[string]any{"error": err.Error()})
				l.safeNack(ctx, lease, nil)
				return
			}
			if !acquired {
				delay := 0
				l.logger.Info("run 正在执行，延后重试", fields, nil)
				l.safeNack(ctx, lease, &delay)
				return
			}
			defer func() {
				if unlock == nil {
					return
				}
				if err := unlock(context.Background()); err != nil {
					l.logger.Error("release advisory lock 失败", fields, map[string]any{"error": err.Error()})
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
		l.logger.Error("heartbeat 连续失败，已停止当前 job", fields, map[string]any{"reason": reason})
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
		l.logger.Info("job 被取消", fields, nil)
		return
	}
	l.logger.Error("job 执行失败，将 nack 重试", fields, map[string]any{"error": runErr.Error()})
	l.safeNack(ctx, lease, nil)
}

func (l *Loop) heartbeatEnabled() bool {
	if l.config.HeartbeatSeconds <= 0 {
		return false
	}
	if l.config.HeartbeatSeconds >= float64(l.config.LeaseSeconds) {
		l.logger.Info("heartbeat_seconds 不应大于等于 lease_seconds，已自动禁用", app.LogFields{}, map[string]any{
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
			l.logger.Error("job heartbeat 失败", fields, map[string]any{"error": err.Error()})
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
		l.logger.Info("ack 失败：lease 已丢失", fieldsFromLease(lease), nil)
		return
	}
	l.logger.Error("ack 失败", fieldsFromLease(lease), map[string]any{"error": err.Error()})
}

func (l *Loop) safeNack(ctx context.Context, lease queue.JobLease, delay *int) {
	err := l.queue.Nack(ctx, lease, delay)
	if err == nil {
		return
	}
	var leaseLost *queue.LeaseLostError
	if errors.As(err, &leaseLost) {
		l.logger.Info("nack 失败：lease 已丢失", fieldsFromLease(lease), nil)
		return
	}
	l.logger.Error("nack 失败", fieldsFromLease(lease), map[string]any{"error": err.Error()})
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
	if value := stringValue(lease.PayloadJSON, "org_id"); value != "" {
		fields.OrgID = stringPtr(value)
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
