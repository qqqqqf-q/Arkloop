package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ThreadReport struct {
	ID         uuid.UUID
	ThreadID   uuid.UUID
	ReporterID uuid.UUID
	Categories []string
	Feedback   *string
	CreatedAt  time.Time
}

type ThreadReportRow struct {
	ThreadReport
	ReporterEmail string
}

type ThreadReportListParams struct {
	ReportID        *uuid.UUID
	ThreadID        *uuid.UUID
	ReporterID      *uuid.UUID
	ReporterEmail   *string
	Category        *string
	FeedbackKeyword *string
	Since           *time.Time
	Until           *time.Time
	Limit           int
	Offset          int
}

type ThreadReportRepository struct {
	db Querier
}

func NewThreadReportRepository(db Querier) (*ThreadReportRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ThreadReportRepository{db: db}, nil
}

func (r *ThreadReportRepository) Create(
	ctx context.Context,
	threadID uuid.UUID,
	reporterID uuid.UUID,
	categories []string,
	feedback *string,
) (*ThreadReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if reporterID == uuid.Nil {
		return nil, fmt.Errorf("reporter_id must not be empty")
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("categories must not be empty")
	}

	var report ThreadReport
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO thread_reports (thread_id, reporter_id, categories, feedback)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, thread_id, reporter_id, categories, feedback, created_at`,
		threadID, reporterID, categories, feedback,
	).Scan(
		&report.ID, &report.ThreadID, &report.ReporterID,
		&report.Categories, &report.Feedback, &report.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (r *ThreadReportRepository) List(
	ctx context.Context,
	params ThreadReportListParams,
) ([]ThreadReportRow, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	conds := make([]string, 0, 8)
	args := make([]any, 0, 10)
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if params.ReportID != nil {
		conds = append(conds, "r.id = "+addArg(*params.ReportID))
	}
	if params.ThreadID != nil {
		conds = append(conds, "r.thread_id = "+addArg(*params.ThreadID))
	}
	if params.ReporterID != nil {
		conds = append(conds, "r.reporter_id = "+addArg(*params.ReporterID))
	}
	if params.ReporterEmail != nil {
		trimmed := strings.TrimSpace(*params.ReporterEmail)
		if trimmed != "" {
			conds = append(conds, "COALESCE(u.email, '') ILIKE '%' || "+addArg(trimmed)+" || '%'")
		}
	}
	if params.Category != nil {
		trimmed := strings.TrimSpace(*params.Category)
		if trimmed != "" {
			conds = append(conds, addArg(trimmed)+" = ANY(r.categories)")
		}
	}
	if params.FeedbackKeyword != nil {
		trimmed := strings.TrimSpace(*params.FeedbackKeyword)
		if trimmed != "" {
			conds = append(conds, "COALESCE(r.feedback, '') ILIKE '%' || "+addArg(trimmed)+" || '%'")
		}
	}
	if params.Since != nil {
		conds = append(conds, "r.created_at >= "+addArg(*params.Since))
	}
	if params.Until != nil {
		conds = append(conds, "r.created_at <= "+addArg(*params.Until))
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	err := r.db.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM thread_reports r
		 LEFT JOIN users u ON u.id = r.reporter_id`+where,
		args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	limitArg := fmt.Sprintf("$%d", len(args)+1)
	offsetArg := fmt.Sprintf("$%d", len(args)+2)

	rows, err := r.db.Query(
		ctx,
		`SELECT r.id, r.thread_id, r.reporter_id, r.categories, r.feedback, r.created_at,
		        COALESCE(u.email, '') AS reporter_email
		 FROM thread_reports r
		 LEFT JOIN users u ON u.id = r.reporter_id
		`+where+`
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT `+limitArg+` OFFSET `+offsetArg,
		pageArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []ThreadReportRow
	for rows.Next() {
		var row ThreadReportRow
		if err := rows.Scan(
			&row.ID, &row.ThreadID, &row.ReporterID,
			&row.Categories, &row.Feedback, &row.CreatedAt,
			&row.ReporterEmail,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return result, total, nil
}
