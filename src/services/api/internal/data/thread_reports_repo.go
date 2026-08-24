package data

import (
	"context"
	"errors"
	"fmt"
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

type ThreadReportSuggestion struct {
	ID        uuid.UUID
	CreatedAt time.Time
}

type ThreadReportRepository struct {
	db Querier
}

const ThreadReportCategoryProductSuggestion = "product_suggestion"

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

func (r *ThreadReportRepository) CreateSuggestion(
	ctx context.Context,
	reporterID uuid.UUID,
	feedback string,
) (*ThreadReportSuggestion, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reporterID == uuid.Nil {
		return nil, fmt.Errorf("reporter_id must not be empty")
	}

	var report ThreadReportSuggestion
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO thread_reports (thread_id, reporter_id, categories, feedback)
		 VALUES (NULL, $1, $2, $3)
		 RETURNING id, created_at`,
		reporterID,
		[]string{ThreadReportCategoryProductSuggestion},
		feedback,
	).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &report, nil
}
