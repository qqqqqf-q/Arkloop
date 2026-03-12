package http

import (
	"context"
	"testing"

	"arkloop/services/api/internal/data"
	"github.com/google/uuid"
)

func mustCreateTestProject(t *testing.T, ctx context.Context, db data.Querier, accountID uuid.UUID, ownerUserID *uuid.UUID, name string) data.Project {
	t.Helper()

	repo, err := data.NewProjectRepository(db)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	project, err := repo.Create(ctx, accountID, nil, name, nil, "private")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if ownerUserID == nil || *ownerUserID == uuid.Nil {
		return project
	}
	if _, err := db.Exec(ctx, `UPDATE projects SET owner_user_id = $2, updated_at = now() WHERE id = $1`, project.ID, *ownerUserID); err != nil {
		t.Fatalf("assign project owner: %v", err)
	}
	project.OwnerUserID = ownerUserID
	return project
}
