ALTER TABLE shared_channel_publications
    ADD COLUMN IF NOT EXISTS models TEXT[] NOT NULL DEFAULT '{}';
