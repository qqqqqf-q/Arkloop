package executor

import (
	"context"
	"fmt"

	"arkloop/services/worker_go/internal/consumer"
	"arkloop/services/worker_go/internal/queue"
)

type JobTypeDispatchHandler struct {
	handlers map[string]consumer.Handler
}

func NewJobTypeDispatchHandler(handlers map[string]consumer.Handler) (*JobTypeDispatchHandler, error) {
	if len(handlers) == 0 {
		return nil, fmt.Errorf("handlers 不能为空")
	}
	copied := make(map[string]consumer.Handler, len(handlers))
	for jobType, handler := range handlers {
		if jobType == "" {
			return nil, fmt.Errorf("job_type 不能为空")
		}
		if handler == nil {
			return nil, fmt.Errorf("handler 不能为空: %s", jobType)
		}
		copied[jobType] = handler
	}
	return &JobTypeDispatchHandler{handlers: copied}, nil
}

func (h *JobTypeDispatchHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	handler := h.handlers[lease.JobType]
	if handler == nil {
		return fmt.Errorf("不支持的 job_type: %s", lease.JobType)
	}
	return handler.Handle(ctx, lease)
}
