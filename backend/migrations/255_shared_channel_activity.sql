-- 使用资格与活跃名单分离：创建 Key 不占份额，真实请求刷新活跃时间。
ALTER TABLE shared_channel_memberships
    ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS active_since TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS shared_channel_active_members_idx
    ON shared_channel_memberships (publication_id, last_active_at)
    WHERE ended_at IS NULL;
