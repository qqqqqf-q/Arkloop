package sandbox

import (
	"context"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type registryService struct {
	pool          *pgxpool.Pool
	profileRepo   data.ProfileRegistriesRepository
	workspaceRepo data.WorkspaceRegistriesRepository
	sessionsRepo  data.ShellSessionsRepository
}

func newRegistryService(pool *pgxpool.Pool) *registryService {
	return &registryService{pool: pool}
}

func (s *registryService) UpsertProfileRegistry(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID *uuid.UUID,
	profileRef string,
	defaultWorkspaceRef *string,
) error {
	if s == nil || s.pool == nil {
		return nil
	}
	profileRef = strings.TrimSpace(profileRef)
	if accountID == uuid.Nil || profileRef == "" {
		return nil
	}
	return s.profileRepo.UpsertTouch(ctx, s.pool, data.RegistryRecord{
		Ref:                 profileRef,
		AccountID:               accountID,
		OwnerUserID:         ownerUserID,
		DefaultWorkspaceRef: defaultWorkspaceRef,
		FlushState:          data.FlushStateIdle,
		LastUsedAt:          time.Now().UTC(),
		MetadataJSON:        map[string]any{},
	})
}

func (s *registryService) UpsertWorkspaceRegistry(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID *uuid.UUID,
	projectID *uuid.UUID,
	workspaceRef string,
	defaultShellSessionRef *string,
) error {
	if s == nil || s.pool == nil {
		return nil
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if accountID == uuid.Nil || workspaceRef == "" {
		return nil
	}
	return s.workspaceRepo.UpsertTouch(ctx, s.pool, data.RegistryRecord{
		Ref:                    workspaceRef,
		AccountID:                  accountID,
		OwnerUserID:            ownerUserID,
		ProjectID:              projectID,
		DefaultShellSessionRef: defaultShellSessionRef,
		FlushState:             data.FlushStateIdle,
		LastUsedAt:             time.Now().UTC(),
		MetadataJSON:           map[string]any{},
	})
}

func (s *registryService) BindSessionRestorePointer(ctx context.Context, accountID uuid.UUID, sessionRef string, revision string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if accountID == uuid.Nil || strings.TrimSpace(sessionRef) == "" {
		return nil
	}
	return s.sessionsRepo.UpdateRestoreRevision(ctx, s.pool, accountID, sessionRef, revision)
}
