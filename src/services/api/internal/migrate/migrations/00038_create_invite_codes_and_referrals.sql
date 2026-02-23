-- +goose Up

CREATE TABLE invite_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT        NOT NULL UNIQUE,
    max_uses   INT         NOT NULL,
    use_count  INT         NOT NULL DEFAULT 0,
    is_active  BOOLEAN     NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invite_codes_user_id ON invite_codes(user_id);

CREATE TABLE referrals (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    inviter_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_user_id UUID        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    invite_code_id  UUID        NOT NULL REFERENCES invite_codes(id) ON DELETE CASCADE,
    credited        BOOLEAN     NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_referrals_inviter_user_id ON referrals(inviter_user_id, created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS referrals;
DROP TABLE IF EXISTS invite_codes;
