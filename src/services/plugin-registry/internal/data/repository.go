package data

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	regmanifest "arkloop/services/plugin-registry/internal/manifest"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Plugin struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Publisher       string    `json:"publisher,omitempty"`
	Description     string    `json:"description,omitempty"`
	HostRequirement string    `json:"host_requirement,omitempty"`
	Platforms       []string  `json:"platforms,omitempty"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Version struct {
	PluginID        string          `json:"plugin_id"`
	Version         string          `json:"version"`
	Manifest        json.RawMessage `json:"manifest"`
	ManifestYAML    string          `json:"-"`
	BundleObjectKey string          `json:"bundle_object_key"`
	BundleSHA256    string          `json:"bundle_sha256"`
	BundleSizeBytes int64           `json:"bundle_size_bytes"`
	CreatedAt       time.Time       `json:"created_at"`
}

type SearchFilter struct {
	Query    string
	Host     string
	Platform string
}

type CreateVersionInput struct {
	Manifest        regmanifest.Manifest
	ManifestJSON    json.RawMessage
	ManifestYAML    string
	BundleObjectKey string
	BundleSHA256    string
	BundleSizeBytes int64
}

type Repository interface {
	SearchPlugins(ctx context.Context, filter SearchFilter) ([]Plugin, error)
	GetPlugin(ctx context.Context, id string) (Plugin, []Version, error)
	GetVersion(ctx context.Context, pluginID string, version string) (Version, error)
	CreateVersion(ctx context.Context, input CreateVersionInput) (Version, error)
}
