package scheduledjobsapi

import (
	"encoding/json"
	"slices"
	"testing"

	"arkloop/services/shared/scheduledjobs"
)

func TestDeleteAfterRunOnlyUpdateStillValidatesOnlyAtConstraint(t *testing.T) {
	value := true
	params := scheduledjobs.UpdateJobParams{
		DeleteAfterRun: &value,
	}

	if !shouldValidateScheduledJobUpdate(params) {
		t.Fatal("expected delete_after_run-only update to trigger validation")
	}

	job := applyJobUpdatePreview(scheduledjobs.ScheduledJobWithTrigger{
		ScheduledJob: scheduledjobs.ScheduledJob{
			Name:         "daily-report",
			Prompt:       "run it",
			PersonaKey:   "ops",
			ScheduleKind: "daily",
			DailyTime:    "08:30",
			Timezone:     "UTC",
		},
	}, params)

	err := validateScheduledJobDefinition(job)
	if err == nil || err.Error() != "delete_after_run is only supported for at schedule" {
		t.Fatalf("expected only-at validation error, got %v", err)
	}
}

func TestBuildUpdateParamsRejectsWrongJSONTypes(t *testing.T) {
	params, errs := buildUpdateParams(map[string]json.RawMessage{
		"name":             json.RawMessage(`123`),
		"thread_id":        json.RawMessage(`true`),
		"interval_min":     json.RawMessage(`"15"`),
		"fire_at":          json.RawMessage(`42`),
		"delete_after_run": json.RawMessage(`"true"`),
		"timeout":          json.RawMessage(`"30"`),
	})

	if params.Name != nil || params.ThreadID != nil || params.IntervalMin != nil || params.FireAt != nil || params.DeleteAfterRun != nil || params.Timeout != nil {
		t.Fatal("expected invalid fields to be rejected")
	}

	expected := []string{
		"name must be a string",
		"thread_id must be a string",
		"interval_min must be an integer",
		"fire_at must be a string",
		"delete_after_run must be a boolean",
		"timeout must be an integer",
	}
	if !slices.Equal(errs, expected) {
		t.Fatalf("unexpected errors: %#v", errs)
	}
}
