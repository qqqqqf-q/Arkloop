-- +goose Up
CREATE TABLE profile_platform_skill_overrides (
    profile_ref  TEXT        NOT NULL,
    skill_key    TEXT        NOT NULL,
    version      TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'manual',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_ref, skill_key, version),
    CONSTRAINT chk_platform_skill_override_status CHECK (status IN ('manual', 'removed'))
);

CREATE INDEX idx_platform_skill_overrides_profile
    ON profile_platform_skill_overrides (profile_ref);

-- +goose Down
DROP TABLE IF EXISTS profile_platform_skill_overrides;
