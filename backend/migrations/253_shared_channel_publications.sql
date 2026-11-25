CREATE TABLE IF NOT EXISTS shared_channel_publications (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL UNIQUE REFERENCES groups (id),
    name VARCHAR(100) NOT NULL CHECK (btrim(name) <> ''),
    max_concurrency INTEGER NOT NULL CHECK (max_concurrency BETWEEN 1 AND 1000),
    expires_at TIMESTAMPTZ,
    published BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (NOT published OR expires_at IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS shared_channel_memberships (
    id BIGSERIAL PRIMARY KEY,
    publication_id BIGINT NOT NULL REFERENCES shared_channel_publications (id),
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ended_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS shared_channel_open_membership_idx
    ON shared_channel_memberships (publication_id, user_id) WHERE ended_at IS NULL;
CREATE INDEX IF NOT EXISTS shared_channel_memberships_user_idx ON shared_channel_memberships (user_id)
    WHERE ended_at IS NULL;

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS shared_channel_membership_id BIGINT
    REFERENCES shared_channel_memberships (id);
CREATE INDEX IF NOT EXISTS api_keys_shared_channel_membership_id_idx ON api_keys (shared_channel_membership_id)
    WHERE shared_channel_membership_id IS NOT NULL;
