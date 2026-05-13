package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) SearchPlugins(ctx context.Context, filter SearchFilter) ([]Plugin, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, name, publisher, description, host_requirement, platforms, latest_version, created_at, updated_at
FROM registry_plugins
WHERE ($1 = '' OR id ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%' OR publisher ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%')
  AND ($2 = '' OR host_requirement = $2)
  AND ($3 = '' OR $3 = ANY(platforms))
ORDER BY id
LIMIT 100`, strings.TrimSpace(filter.Query), strings.TrimSpace(filter.Host), strings.TrimSpace(filter.Platform))
	if err != nil {
		return nil, fmt.Errorf("search registry plugins: %w", err)
	}
	defer rows.Close()

	plugins := []Plugin{}
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search registry plugins rows: %w", err)
	}
	return plugins, nil
}

func (r *PostgresRepository) GetPlugin(ctx context.Context, id string) (Plugin, []Version, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, name, publisher, description, host_requirement, platforms, latest_version, created_at, updated_at
FROM registry_plugins
WHERE id = $1`, id)
	plugin, err := scanPlugin(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plugin{}, nil, ErrNotFound
	}
	if err != nil {
		return Plugin{}, nil, err
	}

	rows, err := r.pool.Query(ctx, `
SELECT plugin_id, version, manifest_json, manifest_yaml, bundle_object_key, bundle_sha256, bundle_size_bytes, created_at
FROM registry_plugin_versions
WHERE plugin_id = $1
ORDER BY created_at DESC`, id)
	if err != nil {
		return Plugin{}, nil, fmt.Errorf("list registry plugin versions: %w", err)
	}
	defer rows.Close()

	versions := []Version{}
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return Plugin{}, nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return Plugin{}, nil, fmt.Errorf("list registry plugin versions rows: %w", err)
	}
	return plugin, versions, nil
}

func (r *PostgresRepository) GetVersion(ctx context.Context, pluginID string, version string) (Version, error) {
	row := r.pool.QueryRow(ctx, `
SELECT plugin_id, version, manifest_json, manifest_yaml, bundle_object_key, bundle_sha256, bundle_size_bytes, created_at
FROM registry_plugin_versions
WHERE plugin_id = $1 AND version = $2`, pluginID, version)
	found, err := scanVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	if err != nil {
		return Version{}, err
	}
	return found, nil
}

func (r *PostgresRepository) CreateVersion(ctx context.Context, input CreateVersionInput) (Version, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("begin registry plugin insert: %w", err)
	}
	defer tx.Rollback(ctx)

	manifestJSON := append(json.RawMessage(nil), input.ManifestJSON...)
	_, err = tx.Exec(ctx, `
INSERT INTO registry_plugins (id, name, publisher, description, host_requirement, platforms, latest_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  publisher = EXCLUDED.publisher,
  description = EXCLUDED.description,
  host_requirement = EXCLUDED.host_requirement,
  platforms = EXCLUDED.platforms,
  latest_version = EXCLUDED.latest_version,
  updated_at = NOW()`,
		input.Manifest.ID,
		input.Manifest.Name,
		input.Manifest.Publisher,
		input.Manifest.Description,
		input.Manifest.HostRequirement,
		input.Manifest.Platforms,
		input.Manifest.Version,
	)
	if err != nil {
		return Version{}, fmt.Errorf("upsert registry plugin: %w", err)
	}

	row := tx.QueryRow(ctx, `
INSERT INTO registry_plugin_versions (
  plugin_id, version, manifest_json, manifest_yaml, bundle_object_key, bundle_sha256, bundle_size_bytes, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
RETURNING plugin_id, version, manifest_json, manifest_yaml, bundle_object_key, bundle_sha256, bundle_size_bytes, created_at`,
		input.Manifest.ID,
		input.Manifest.Version,
		manifestJSON,
		input.ManifestYAML,
		input.BundleObjectKey,
		input.BundleSHA256,
		input.BundleSizeBytes,
	)
	version, err := scanVersion(row)
	if isUniqueViolation(err) {
		return Version{}, ErrConflict
	}
	if err != nil {
		return Version{}, fmt.Errorf("insert registry plugin version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("commit registry plugin insert: %w", err)
	}
	return version, nil
}

type pluginScanner interface {
	Scan(dest ...any) error
}

func scanPlugin(row pluginScanner) (Plugin, error) {
	var plugin Plugin
	if err := row.Scan(
		&plugin.ID,
		&plugin.Name,
		&plugin.Publisher,
		&plugin.Description,
		&plugin.HostRequirement,
		&plugin.Platforms,
		&plugin.LatestVersion,
		&plugin.CreatedAt,
		&plugin.UpdatedAt,
	); err != nil {
		return Plugin{}, err
	}
	return plugin, nil
}

func scanVersion(row pluginScanner) (Version, error) {
	var version Version
	if err := row.Scan(
		&version.PluginID,
		&version.Version,
		&version.Manifest,
		&version.ManifestYAML,
		&version.BundleObjectKey,
		&version.BundleSHA256,
		&version.BundleSizeBytes,
		&version.CreatedAt,
	); err != nil {
		return Version{}, err
	}
	return version, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("postgres pool must not be nil")
	}
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS registry_plugins (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  publisher TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  host_requirement TEXT NOT NULL DEFAULT '',
  platforms TEXT[] NOT NULL DEFAULT '{}',
  latest_version TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS registry_plugin_versions (
  plugin_id TEXT NOT NULL REFERENCES registry_plugins(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  manifest_json JSONB NOT NULL,
  manifest_yaml TEXT NOT NULL DEFAULT '',
  bundle_object_key TEXT NOT NULL DEFAULT '',
  bundle_sha256 TEXT NOT NULL DEFAULT '',
  bundle_size_bytes BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (plugin_id, version)
);

CREATE INDEX IF NOT EXISTS idx_registry_plugins_platforms ON registry_plugins USING GIN (platforms);`)
	if err != nil {
		return fmt.Errorf("ensure registry schema: %w", err)
	}
	return nil
}
