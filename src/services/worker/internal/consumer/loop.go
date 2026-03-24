package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

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
	logger   *slog.Logger
	notifier WorkNotifier

	// targetWorkers is the desired number of running goroutines. Accessed only
	// via atomic operations. mu protects scaleCooldown exclusively.
	targetWorkers atomic.Int32
	mu            sync.Mutex
	scaleCooldown time.Time
}

func NewLoop(
	queueClient queue.JobQueue,
	handler Handler,
	locker RunLocker,
	config Config,
	logger *slog.Logger,
	notifier WorkNotifier,
) (*Loop, error) {
	if queueClient == nil {
		return nil, fmt.Errorf("queue must not be nil")
	}
	if handler == nil {
		return nil, fmt.Errorf("handler must not be nil")
	}

	// Fill adaptive defaults for callers that don't set them.
	if config.MinConcurrency <= 0 {
		config.MinConcurrency = 2
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 16
	}
	if config.MaxConcurrency < config.MinConcurrency {
		config.MaxConcurrency = config.MinConcurrency
	}
	if config.ScaleUpThreshold <= 0 {
		config.ScaleUpThreshold = 3
	}
	if config.ScaleDownThreshold < 0 {
		config.ScaleDownThreshold = 1
	}
	if config.ScaleIntervalSecs <= 0 {
		config.ScaleIntervalSecs = 5
	}
	if config.ScaleCooldownSecs <= 0 {
		config.ScaleCooldownSecs = 30
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	initial := config.Concurrency
	if initial < config.MinConcurrency {
		initial = config.MinConcurrency
	}
	if initial > config.MaxConcurrency {
		initial = config.MaxConcurrency
	}

	l := &Loop{
		queue:    queueClient,
		handler:  handler,
		locker:   locker,
		config:   config,
		logger:   logger,
		notifier: notifier,
	}
	l.targetWorkers.Store(int32(initial))
	return l, nil
}

// Run starts the consumer loop with adaptive concurrency and blocks until ctx
// is cancelled or a fatal error occurs.
func (l *Loop) Run(ctx context.Context) error {
	var (
		wg            sync.WaitGroup
		activeWorkers atomic.Int32
	)

	// spawnWorker starts one worker goroutine.
	spawnWorker := func() {
		activeWorkers.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer activeWorkers.Add(-1)
			for {
				// Scale-down: exit if we are over the current target.
				if int(activeWorkers.Load()) > int(l.targetWorkers.Load()) {
					return
				}

				select {
				case <-ctx.Done():
					return
				default:
				}

				processed, err := l.RunOnce(ctx)
				if err != nil {
					// Only ctx cancellation reaches here; RunOnce swallows transient errors.
					return
				}

				if processed {
					continue
				}

				l.waitForWork(ctx)
			}
		}()
	}

	// Spawn initial workers.
	initial := int(l.targetWorkers.Load())
	for i := 0; i < initial; i++ {
		spawnWorker()
	}

	// Scale monitor goroutine.
	scaleDone := make(chan struct{})
	go func() {
		defer close(scaleDone)
		interval := time.Duration(l.config.ScaleIntervalSecs * float64(time.Second))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				active := int(activeWorkers.Load())
				newTarget := l.evaluateScaling(ctx, active)
				oldTarget := int(l.targetWorkers.Swap(int32(newTarget)))
				// Spawn workers for any growth in target.
				// Use the delta between old and new target, not activeWorkers,
				// because self-exiting workers decrement activeWorkers asynchronously.
				delta := newTarget - oldTarget
				for i := 0; i < delta; i++ {
					spawnWorker()
				}
				// Scale-down: existing goroutines self-exit on next iteration check.
			}
		}
	}()

	<-ctx.Done()
	<-scaleDone
	wg.Wait()
	return nil
}

// evaluateScaling computes the new target worker count based on queue depth.
// mu protects scaleCooldown to prevent concurrent scale decisions.
func (l *Loop) evaluateScaling(ctx context.Context, active int) int {
	current := int(l.targetWorkers.Load())
	if active <= 0 {
		active = 1
	}

	stats, err := l.gatherQueueStats(ctx, active)
	if err != nil {
		l.logger.Error("queue stats query failed", "error", err.Error())
		return current
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if stats.ReadyDepth > 0 && stats.InFlight >= active && current < l.config.MaxConcurrency && now.After(l.scaleCooldown) {
		next := current + 1
		if next > l.config.MaxConcurrency {
			next = l.config.MaxConcurrency
		}
		if next == current {
			return current
		}

		l.scaleCooldown = now.Add(time.Duration(l.config.ScaleCooldownSecs * float64(time.Second)))
		l.logger.Info("scaling up workers",
			"from", current,
			"to", next,
			"ready_depth", stats.ReadyDepth,
			"in_flight", stats.InFlight,
			"oldest_ready_age", stats.OldestReadyAge,
		)
		return next
	}

	if stats.ReadyDepth == 0 && stats.InFlight < active && current > l.config.MinConcurrency && now.After(l.scaleCooldown) {
		next := current - 1
		l.scaleCooldown = now.Add(time.Duration(l.config.ScaleCooldownSecs * float64(time.Second)))
		l.logger.Info("scaling down workers",
			"from", current,
			"to", next,
			"ready_depth", stats.ReadyDepth,
			"in_flight", stats.InFlight,
		)
		return next
	}

	return current
}

func (l *Loop) gatherQueueStats(ctx context.Context, active int) (queue.QueueStats, error) {
	if provider, ok := l.queue.(queue.QueueStatsProvider); ok {
		return provider.QueueStats(ctx, l.config.QueueJobTypes)
	}
	depth, err := l.queue.QueueDepth(ctx, l.config.QueueJobTypes)
	if err != nil {
		return queue.QueueStats{}, err
	}
	if active < 0 {
		active = 0
	}
	return queue.QueueStats{
		ReadyDepth: depth,
		InFlight:   active,
	}, nil
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
		// Treat transient Lease errors (DB blip, pool timeout) as no-op so the
		// worker goroutine backs off via waitForWork rather than fatally exits.
		l.logger.Error("lease job 失败", "error", err.Error())
		return false, nil
	}
	if lease == nil {
		return false, nil
	}

	l.processLease(ctx, *lease)
	return true, nil
}

func (l *Loop) processLease(ctx context.Context, lease queue.JobLease) {
	jobID := lease.JobID.String()
	traceID := stringValue(lease.PayloadJSON, "trace_id")
	accountID := stringValue(lease.PayloadJSON, "account_id")
	runID := stringValue(lease.PayloadJSON, "run_id")

	payloadType := stringValue(lease.PayloadJSON, "type")
	if l.locker != nil && payloadType == queue.RunExecuteJobType {
		parsedRunID, ok := extractRunID(lease.PayloadJSON)
		if ok {
			unlock, acquired, err := l.locker.TryAcquire(ctx, parsedRunID)
			if err != nil {
				l.logger.Error("acquire advisory lock failed", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID, "error", err.Error())
				l.safeNack(ctx, lease, nil)
				return
			}
			if !acquired {
				l.logger.Info("run already executing, deferring retry", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID)
				l.safeNack(ctx, lease, nil)
				return
			}
			defer func() {
				if unlock == nil {
					return
				}
				if err := unlock(context.Background()); err != nil {
					l.logger.Error("release advisory lock failed", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID, "error", err.Error())
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
		l.logger.Error("heartbeat consecutive failures, stopped current job", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID, "reason", reason)
		l.safeNack(ctx, lease, nil)
	}
}

func (l *Loop) settleJob(ctx context.Context, lease queue.JobLease, runErr error) {
	jobID := lease.JobID.String()
	traceID := stringValue(lease.PayloadJSON, "trace_id")
	accountID := stringValue(lease.PayloadJSON, "account_id")
	runID := stringValue(lease.PayloadJSON, "run_id")

	if runErr == nil {
		l.safeAck(ctx, lease)
		return
	}
	if errors.Is(runErr, context.Canceled) {
		l.logger.Info("job cancelled", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID)
		return
	}
	l.logger.Error("job execution failed, will nack for retry", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID, "error", runErr.Error())
	l.safeNack(ctx, lease, nil)
}

func (l *Loop) heartbeatEnabled() bool {
	if l.config.HeartbeatSeconds <= 0 {
		return false
	}
	if l.config.HeartbeatSeconds >= float64(l.config.LeaseSeconds) {
		l.logger.Info("heartbeat_seconds must be less than lease_seconds, auto-disabled",
			"heartbeat_seconds", l.config.HeartbeatSeconds,
			"lease_seconds", l.config.LeaseSeconds,
		)
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
	jobID := lease.JobID.String()
	traceID := stringValue(lease.PayloadJSON, "trace_id")
	accountID := stringValue(lease.PayloadJSON, "account_id")
	runID := stringValue(lease.PayloadJSON, "run_id")

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
			l.logger.Error("job heartbeat failed", "job_id", jobID, "trace_id", traceID, "account_id", accountID, "run_id", runID, "error", err.Error())
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
		l.logger.Info("ack failed: lease lost", "job_id", lease.JobID.String())
		return
	}
	l.logger.Error("ack failed", "job_id", lease.JobID.String(), "error", err.Error())
}

func (l *Loop) safeNack(ctx context.Context, lease queue.JobLease, delay *int) {
	err := l.queue.Nack(ctx, lease, delay)
	if err == nil {
		return
	}
	var leaseLost *queue.LeaseLostError
	if errors.As(err, &leaseLost) {
		l.logger.Info("nack failed: lease lost", "job_id", lease.JobID.String())
		return
	}
	l.logger.Error("nack failed", "job_id", lease.JobID.String(), "error", err.Error())
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
