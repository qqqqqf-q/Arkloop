-- +goose Up
WITH ranked_jobs AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY payload_json->>'run_id'
               ORDER BY created_at ASC, id ASC
           ) AS row_num
      FROM jobs
     WHERE job_type = 'run.execute'
       AND status IN ('queued', 'leased')
)
DELETE FROM jobs
 WHERE id IN (
    SELECT id
      FROM ranked_jobs
     WHERE row_num > 1
 );

CREATE UNIQUE INDEX ux_jobs_run_execute_active_run
    ON jobs ((payload_json->>'run_id'))
    WHERE job_type = 'run.execute' AND status IN ('queued', 'leased');

-- +goose Down
DROP INDEX IF EXISTS ux_jobs_run_execute_active_run;
