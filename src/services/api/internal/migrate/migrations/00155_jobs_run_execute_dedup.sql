-- +goose Up
CREATE UNIQUE INDEX ux_jobs_run_execute_active_run
    ON jobs ((payload_json->>'run_id'))
    WHERE job_type = 'run.execute' AND status IN ('queued', 'leased');

-- +goose Down
DROP INDEX IF EXISTS ux_jobs_run_execute_active_run;
