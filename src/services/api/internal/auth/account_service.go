package auth

import (
	"context"
	"fmt"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AccountService struct {
	pool           *pgxpool.Pool
	accountRepo    *data.AccountRepository
	membershipRepo *data.AccountMembershipRepository
}

func NewAccountService(pool *pgxpool.Pool, accountRepo *data.AccountRepository, membershipRepo *data.AccountMembershipRepository) (*AccountService, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if accountRepo == nil {
		return nil, fmt.Errorf("accountRepo must not be nil")
	}
	if membershipRepo == nil {
		return nil, fmt.Errorf("membershipRepo must not be nil")
	}
	return &AccountService{pool: pool, accountRepo: accountRepo, membershipRepo: membershipRepo}, nil
}

type CreateWorkspaceResult struct {
	Account    data.Account
	Membership data.AccountMembership
}

// CreateWorkspace 创建 workspace 类型 account，在事务内将 ownerUserID 设为 owner。
func (s *AccountService) CreateWorkspace(ctx context.Context, slug, name string, ownerUserID uuid.UUID) (CreateWorkspaceResult, error) {
	if slug == "" {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service: slug must not be empty")
	}
	if name == "" {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service: name must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service: ownerUserID must not be empty")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service.CreateWorkspace: %w", err)
	}
	defer tx.Rollback(ctx)

	accountRepo, err := data.NewAccountRepository(tx)
	if err != nil {
		return CreateWorkspaceResult{}, err
	}
	membershipRepo, err := data.NewAccountMembershipRepository(tx)
	if err != nil {
		return CreateWorkspaceResult{}, err
	}

	account, err := accountRepo.Create(ctx, slug, name, "workspace")
	if err != nil {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service.CreateWorkspace: create account: %w", err)
	}

	membership, err := membershipRepo.Create(ctx, account.ID, ownerUserID, "owner")
	if err != nil {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service.CreateWorkspace: create membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return CreateWorkspaceResult{}, fmt.Errorf("account_service.CreateWorkspace: commit: %w", err)
	}

	return CreateWorkspaceResult{Account: account, Membership: membership}, nil
}
