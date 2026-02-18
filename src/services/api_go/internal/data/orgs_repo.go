package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Org struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	CreatedAt time.Time
}

type OrgRepository struct {
	db Querier
}

func NewOrgRepository(db Querier) (*OrgRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &OrgRepository{db: db}, nil
}

func (r *OrgRepository) Create(ctx context.Context, slug string, name string) (Org, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if slug == "" {
		return Org{}, fmt.Errorf("slug 不能为空")
	}
	if name == "" {
		return Org{}, fmt.Errorf("name 不能为空")
	}

	var org Org
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO orgs (slug, name)
		 VALUES ($1, $2)
		 RETURNING id, slug, name, created_at`,
		slug,
		name,
	).Scan(&org.ID, &org.Slug, &org.Name, &org.CreatedAt)
	if err != nil {
		return Org{}, err
	}
	return org, nil
}
