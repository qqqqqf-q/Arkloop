package pipeline

import (
	"context"
	"strings"

	"arkloop/services/shared/runkind"
)

// isScheduledJobRun 检查 run_kind=scheduled_job
func isScheduledJobRun(input, job map[string]any) bool {
	if s, ok := stringField(input, "run_kind"); ok && strings.EqualFold(s, runkind.ScheduledJob) {
		return true
	}
	if s, ok := stringField(job, "run_kind"); ok && strings.EqualFold(s, runkind.ScheduledJob) {
		return true
	}
	return false
}

// NewScheduledJobPrepareMiddleware 为 scheduled_job run 注入任务身份上下文。
// 用户 prompt 由 scheduler 作为真实 user message 写入 messages 表，
// 会通过正常 thread history 加载进入 LLM 上下文，这里不再重复注入。
func NewScheduledJobPrepareMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !isScheduledJobRun(rc.InputJSON, rc.JobPayload) {
			return next(ctx, rc)
		}

		rc.ScheduledJobRun = true

		jobID, _ := stringField(rc.JobPayload, "scheduled_job_id")
		jobName, _ := stringField(rc.JobPayload, "scheduled_job_name")

		// system prefix: 任务身份上下文
		rc.UpsertPromptSegment(PromptSegment{
			Name:          "scheduled_job.context",
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          "[SCHEDULED_JOB]\nname: " + jobName + "\njob_id: " + jobID + "\n[/SCHEDULED_JOB]",
			Stability:     PromptStabilitySessionPrefix,
			CacheEligible: true,
		})

		// 覆盖 workspace/workdir
		if wsRef, ok := stringField(rc.JobPayload, "workspace_ref"); ok {
			rc.WorkspaceRef = wsRef
		}
		if wd, ok := stringField(rc.JobPayload, "work_dir"); ok {
			rc.WorkDir = wd
		}

		return next(ctx, rc)
	}
}
