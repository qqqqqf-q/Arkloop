package data

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu       sync.RWMutex
	plugins  map[string]Plugin
	versions map[string]map[string]Version
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		plugins:  map[string]Plugin{},
		versions: map[string]map[string]Version{},
	}
}

func (r *MemoryRepository) SearchPlugins(_ context.Context, filter SearchFilter) ([]Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Plugin, 0, len(r.plugins))
	for _, plugin := range r.plugins {
		if !matchesPlugin(plugin, filter) {
			continue
		}
		out = append(out, clonePlugin(plugin))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *MemoryRepository) GetPlugin(_ context.Context, id string) (Plugin, []Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.plugins[id]
	if !ok {
		return Plugin{}, nil, ErrNotFound
	}
	versions := make([]Version, 0, len(r.versions[id]))
	for _, version := range r.versions[id] {
		versions = append(versions, cloneVersion(version))
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].CreatedAt.After(versions[j].CreatedAt)
	})
	return clonePlugin(plugin), versions, nil
}

func (r *MemoryRepository) GetVersion(_ context.Context, pluginID string, version string) (Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byVersion := r.versions[pluginID]
	if byVersion == nil {
		return Version{}, ErrNotFound
	}
	found, ok := byVersion[version]
	if !ok {
		return Version{}, ErrNotFound
	}
	return cloneVersion(found), nil
}

func (r *MemoryRepository) CreateVersion(_ context.Context, input CreateVersionInput) (Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pluginID := input.Manifest.ID
	versionID := input.Manifest.Version
	if r.versions[pluginID] != nil {
		if _, exists := r.versions[pluginID][versionID]; exists {
			return Version{}, ErrConflict
		}
	}

	now := time.Now().UTC()
	plugin := Plugin{
		ID:              pluginID,
		Name:            input.Manifest.Name,
		Publisher:       input.Manifest.Publisher,
		Description:     input.Manifest.Description,
		HostRequirement: input.Manifest.HostRequirement,
		Platforms:       append([]string(nil), input.Manifest.Platforms...),
		LatestVersion:   versionID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if existing, ok := r.plugins[pluginID]; ok {
		plugin.CreatedAt = existing.CreatedAt
	}
	r.plugins[pluginID] = plugin

	if r.versions[pluginID] == nil {
		r.versions[pluginID] = map[string]Version{}
	}
	version := Version{
		PluginID:        pluginID,
		Version:         versionID,
		Manifest:        append(json.RawMessage(nil), input.ManifestJSON...),
		ManifestYAML:    input.ManifestYAML,
		BundleObjectKey: input.BundleObjectKey,
		BundleSHA256:    input.BundleSHA256,
		BundleSizeBytes: input.BundleSizeBytes,
		CreatedAt:       now,
	}
	r.versions[pluginID][versionID] = version
	return cloneVersion(version), nil
}

func matchesPlugin(plugin Plugin, filter SearchFilter) bool {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query != "" {
		haystack := strings.ToLower(plugin.ID + "\n" + plugin.Name + "\n" + plugin.Publisher + "\n" + plugin.Description)
		if !strings.Contains(haystack, query) {
			return false
		}
	}
	if filter.Host != "" && plugin.HostRequirement != filter.Host {
		return false
	}
	if filter.Platform != "" {
		found := false
		for _, platform := range plugin.Platforms {
			if platform == filter.Platform {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func clonePlugin(plugin Plugin) Plugin {
	plugin.Platforms = append([]string(nil), plugin.Platforms...)
	return plugin
}

func cloneVersion(version Version) Version {
	version.Manifest = append(json.RawMessage(nil), version.Manifest...)
	return version
}
