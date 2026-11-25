ALTER TABLE shared_channel_publications
    ADD COLUMN IF NOT EXISTS budget_limit NUMERIC(20,8)
        CHECK (budget_limit > 0 AND budget_limit <= 1000000000),
    ADD COLUMN IF NOT EXISTS budget_multiplier NUMERIC(10,6) NOT NULL DEFAULT 1
        CHECK (budget_multiplier BETWEEN 0.000001 AND 1000),
    ADD COLUMN IF NOT EXISTS budget_used NUMERIC(24,8) NOT NULL DEFAULT 0
        CHECK (budget_used >= 0),
    ADD COLUMN IF NOT EXISTS budget_pricing_blocked BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE shared_channel_publications DROP CONSTRAINT IF EXISTS shared_channel_publications_check;
ALTER TABLE shared_channel_publications DROP CONSTRAINT IF EXISTS shared_channel_requires_end_condition;
ALTER TABLE shared_channel_publications ADD CONSTRAINT shared_channel_requires_end_condition
    CHECK (NOT published OR expires_at IS NOT NULL OR budget_limit IS NOT NULL);

ALTER TABLE shared_channel_publications DROP CONSTRAINT IF EXISTS shared_channel_single_end_condition;
ALTER TABLE shared_channel_publications ADD CONSTRAINT shared_channel_single_end_condition
    CHECK (expires_at IS NULL OR budget_limit IS NULL);
