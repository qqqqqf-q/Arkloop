package scheduledjobs

import (
	"time"

	"github.com/google/uuid"
)

// ScheduledJob 是 scheduled_jobs 表的行。
type ScheduledJob struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	Name            string
	Description     string
	PersonaKey      string
	Prompt          string
	Model           string
	WorkspaceRef    string
	WorkDir         string
	ThreadID        *uuid.UUID
	ScheduleKind    string
	IntervalMin     *int
	DailyTime       string
	MonthlyDay      *int
	MonthlyTime     string
	WeeklyDay       *int
	FireAt          *time.Time
	CronExpr        string
	Timezone        string
	Enabled         bool
	DeleteAfterRun  bool
	Thinking        bool
	Timeout         int
	LightContext    bool
	ToolsAllow      string
	CreatedByUserID *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ScheduledJobWithTrigger 附带 trigger 的 next_fire_at。
type ScheduledJobWithTrigger struct {
	ScheduledJob
	NextFireAt *time.Time
}

// UpdateJobParams 是 UpdateJob 的部分更新参数。
type UpdateJobParams struct {
	Name         *string
	Description  *string
	PersonaKey   *string
	Prompt       *string
	Model        *string
	WorkspaceRef *string
	WorkDir      *string
	ThreadID     **uuid.UUID
	ScheduleKind *string
	IntervalMin  **int
	DailyTime    *string
	MonthlyDay   **int
	MonthlyTime  *string
	WeeklyDay    **int
	FireAt       **time.Time
	CronExpr       *string
	Timezone       *string
	Enabled        *bool
	DeleteAfterRun *bool
	Thinking       *bool
	Timeout        *int
	LightContext   *bool
	ToolsAllow     *string
}
