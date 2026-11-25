ALTER TABLE shared_channel_publications ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE shared_channel_publications DROP CONSTRAINT IF EXISTS shared_channel_publications_group_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS shared_channel_publications_group_id_key
    ON shared_channel_publications (group_id) WHERE deleted_at IS NULL;
