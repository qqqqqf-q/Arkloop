package data

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SkillsRepository struct{}

func (SkillsRepository) ResolveEnabledSkills(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if profileRef == "" || workspaceRef == "" {
		return nil, nil
	}
	rows, err := pool.Query(
		ctx,
		`SELECT sp.skill_key,
		        sp.version,
		        sp.manifest_key,
		        sp.bundle_key,
		        sp.instruction_path
		   FROM workspace_skill_enablements wse
		   JOIN profile_skill_installs psi
		     ON psi.account_id = wse.account_id
		    AND psi.profile_ref = $2
		    AND psi.skill_key = wse.skill_key
		    AND psi.version = wse.version
		   JOIN skill_packages sp
		     ON sp.account_id = wse.account_id
		    AND sp.skill_key = wse.skill_key
		    AND sp.version = wse.version
		  WHERE wse.account_id = $1
		    AND wse.workspace_ref = $3
		    AND sp.is_active = TRUE
		  ORDER BY sp.skill_key, sp.version`,
		accountID,
		profileRef,
		workspaceRef,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]skillstore.ResolvedSkill, 0)
	for rows.Next() {
		var item skillstore.ResolvedSkill
		if err := rows.Scan(&item.SkillKey, &item.Version, &item.ManifestRef, &item.BundleRef, &item.InstructionPath); err != nil {
			return nil, err
		}
		item.MountPath = skillstore.MountPath(item.SkillKey, item.Version)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
