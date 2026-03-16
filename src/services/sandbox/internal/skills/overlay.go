package skills

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"arkloop/services/shared/skillstore"
)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type Carrier interface {
	ApplySkillOverlay(ctx context.Context, req ApplyRequest) error
}

type ApplyRequest struct {
	Skills    []ApplySkill
	IndexJSON string
}

type ApplySkill struct {
	SkillKey         string
	Version          string
	MountPath        string
	InstructionPath  string
	BundleDataBase64 string
}

type OverlayManager struct {
	store Store
}

func NewOverlayManager(store Store) *OverlayManager {
	return &OverlayManager{store: store}
}

func (m *OverlayManager) Apply(ctx context.Context, carrier Carrier, skills []skillstore.ResolvedSkill) error {
	if carrier == nil {
		return nil
	}
	normalized := normalizeResolvedSkills(skills)
	indexJSON, err := skillstore.BuildIndex(normalized)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return carrier.ApplySkillOverlay(ctx, ApplyRequest{IndexJSON: string(indexJSON)})
	}
	if m == nil || m.store == nil {
		return fmt.Errorf("skill store not configured")
	}
	items := make([]ApplySkill, 0, len(normalized))
	for _, item := range normalized {
		bundleRef := strings.TrimSpace(item.BundleRef)
		if bundleRef == "" {
			return fmt.Errorf("skill %s@%s bundle_ref is empty", item.SkillKey, item.Version)
		}
		encoded, err := m.store.Get(ctx, bundleRef)
		if err != nil {
			return err
		}
		bundle, err := skillstore.DecodeBundle(encoded)
		if err != nil {
			return fmt.Errorf("decode skill bundle %s@%s: %w", item.SkillKey, item.Version, err)
		}
		if bundle.Definition.SkillKey != strings.TrimSpace(item.SkillKey) || bundle.Definition.Version != strings.TrimSpace(item.Version) {
			return fmt.Errorf("skill bundle %s has mismatched definition", bundleRef)
		}
		items = append(items, ApplySkill{
			SkillKey:         item.SkillKey,
			Version:          item.Version,
			MountPath:        skillstore.MountPath(item.SkillKey, item.Version),
			InstructionPath:  bundle.Definition.InstructionPath,
			BundleDataBase64: base64.StdEncoding.EncodeToString(encoded),
		})
	}
	return carrier.ApplySkillOverlay(ctx, ApplyRequest{Skills: items, IndexJSON: string(indexJSON)})
}

func normalizeResolvedSkills(values []skillstore.ResolvedSkill) []skillstore.ResolvedSkill {
	if len(values) == 0 {
		return nil
	}
	out := make([]skillstore.ResolvedSkill, 0, len(values))
	seen := map[string]struct{}{}
	for _, item := range values {
		item.SkillKey = strings.TrimSpace(item.SkillKey)
		item.Version = strings.TrimSpace(item.Version)
		if item.SkillKey == "" || item.Version == "" {
			continue
		}
		key := item.SkillKey + "@" + item.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(item.MountPath) == "" {
			item.MountPath = skillstore.MountPath(item.SkillKey, item.Version)
		}
		out = append(out, item)
	}
	return out
}
