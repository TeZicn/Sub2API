package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const sharedChannelGroupEligible = `g.deleted_at IS NULL AND g.status = 'active' AND g.is_exclusive AND g.subscription_type = 'standard'`
const sharedChannelLive = `p.deleted_at IS NULL AND p.published
	AND (p.expires_at IS NULL OR p.expires_at > statement_timestamp())
	AND NOT p.budget_pricing_blocked
	AND (p.budget_limit IS NULL OR p.budget_used < p.budget_limit)
	AND ` + sharedChannelGroupEligible
const sharedChannelFields = `p.id, p.name, p.group_id, p.max_concurrency, p.expires_at, p.published,
	g.name, g.platform, (` + sharedChannelGroupEligible + `),
	p.budget_limit, p.budget_multiplier, p.budget_used, p.budget_pricing_blocked, p.models`
const sharedChannelJoin = ` FROM shared_channel_publications p JOIN groups g ON g.id = p.group_id `

// 所有展示和分配查询共用同一活跃窗口，避免名单与请求份额不一致。
const sharedMembershipActive = `ended_at IS NULL AND last_active_at > statement_timestamp() - interval '2 minutes'`

type sharedChannelRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewSharedChannelRepository(client *dbent.Client, db *sql.DB) service.SharedChannelRepository {
	return &sharedChannelRepository{client: client, db: db}
}

type sharedRow interface{ Scan(...any) error }

func scanSharedChannel(row sharedRow) (c service.SharedChannel, err error) {
	err = row.Scan(&c.ID, &c.Name, &c.GroupID, &c.MaxConcurrency, &c.ExpiresAt, &c.Published,
		&c.GroupName, &c.Platform, &c.GroupEligible, &c.BudgetLimit, &c.BudgetMultiplier, &c.BudgetUsed, &c.BudgetPricingBlocked, pq.Array(&c.Models))
	return
}

func (r *sharedChannelRepository) List(ctx context.Context) ([]service.SharedChannel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sharedChannelFields+sharedChannelJoin+`WHERE p.deleted_at IS NULL ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.SharedChannel, 0)
	for rows.Next() {
		c, err := scanSharedChannel(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *sharedChannelRepository) ListPublished(ctx context.Context, userID int64) ([]service.PublishedSharedChannel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id, p.group_id, p.name, g.platform, p.max_concurrency, p.expires_at,
		COALESCE(m.names, '{}'), COALESCE(m.rank, 0), p.budget_limit, p.budget_used, viewer.concurrency,
		EXISTS (SELECT 1 FROM shared_channel_memberships joined
			WHERE joined.publication_id = p.id AND joined.user_id = $1 AND joined.ended_at IS NULL), p.models
		`+sharedChannelJoin+`
		JOIN users viewer ON viewer.id = $1 AND viewer.deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT array_agg(COALESCE(NULLIF(btrim(u.username), ''), split_part(u.email, '@', 1)) ORDER BY sm.rank) AS names,
				max(sm.rank) FILTER (WHERE sm.user_id = $1) AS rank
			FROM (
				SELECT user_id, id, row_number() OVER (ORDER BY active_since, id) AS rank
				FROM shared_channel_memberships WHERE publication_id = p.id AND `+sharedMembershipActive+`
			) sm JOIN users u ON u.id = sm.user_id
		) m ON TRUE
		WHERE `+sharedChannelLive+` ORDER BY p.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.PublishedSharedChannel, 0)
	positions := make(map[int64]int)
	ids := make([]int64, 0)
	for rows.Next() {
		var c service.PublishedSharedChannel
		var rank, personalConcurrency int
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.Platform, &c.MaxConcurrency, &c.ExpiresAt,
			pq.Array(&c.Usernames), &rank, &c.BudgetLimit, &c.BudgetUsed, &personalConcurrency, &c.Joined, pq.Array(&c.Models)); err != nil {
			return nil, err
		}
		members := len(c.Usernames)
		c.Share = (service.SharedChannelMembership{MaxConcurrency: c.MaxConcurrency, Members: members, Rank: rank, PersonalConcurrency: personalConcurrency}).Share()
		c.Hours = []service.SharedChannelHour{}
		positions[c.ID] = len(result)
		ids = append(ids, c.ID)
		result = append(result, c)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return result, err
	}
	// 沿用网关记录的错误归属，只统计最终上游失败；恢复成功的尝试和用户业务限制不影响状态。
	stats, err := r.db.QueryContext(ctx, `WITH linked AS (
		SELECT ak.id AS key_id, m.publication_id
		FROM shared_channel_memberships m JOIN api_keys ak ON ak.shared_channel_membership_id = m.id
		WHERE m.publication_id = ANY($1)
	), events AS (
		SELECT k.publication_id, date_trunc('hour', u.created_at) AS bucket, FALSE AS failed FROM linked k
		JOIN usage_logs u ON u.api_key_id = k.key_id
		WHERE u.created_at >= date_trunc('hour', statement_timestamp()) - interval '23 hours'
		UNION ALL
		SELECT k.publication_id, date_trunc('hour', e.created_at), TRUE FROM linked k
		JOIN ops_error_logs e ON e.api_key_id = k.key_id
		WHERE e.created_at >= date_trunc('hour', statement_timestamp()) - interval '23 hours'
			AND e.status_code >= 400 AND e.status_code <> 499
			AND e.error_owner = 'provider' AND NOT e.is_business_limited
	), hours AS (
		SELECT generate_series(date_trunc('hour', statement_timestamp()) - interval '23 hours',
			date_trunc('hour', statement_timestamp()), interval '1 hour') AS bucket
	)
	SELECT channels.publication_id, hours.bucket, count(events.bucket),
		count(events.bucket) FILTER (WHERE events.failed)
	FROM unnest($1::bigint[]) AS channels(publication_id)
	CROSS JOIN hours
	LEFT JOIN events ON events.publication_id = channels.publication_id AND events.bucket = hours.bucket
	GROUP BY channels.publication_id, hours.bucket ORDER BY channels.publication_id, hours.bucket`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = stats.Close() }()
	for stats.Next() {
		var id int64
		var hour service.SharedChannelHour
		if err := stats.Scan(&id, &hour.StartedAt, &hour.Requests, &hour.Failures); err != nil {
			return nil, err
		}
		i := positions[id]
		result[i].Hours = append(result[i].Hours, hour)
	}
	return result, stats.Err()
}

func sharedQueryOne(ctx context.Context, client *dbent.Client, query string, args []any, dest ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return rows.Scan(dest...)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return sql.ErrNoRows
}

func (r *sharedChannelRepository) Save(ctx context.Context, id int64, in service.SharedChannelInput) (*service.SharedChannel, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	if id > 0 {
		if err := updateSharedPublication(ctx, client, id, in); err != nil {
			return nil, err
		}
	} else {
		err := sharedQueryOne(ctx, client, `INSERT INTO shared_channel_publications
			(name, group_id, max_concurrency, expires_at, published, budget_limit, budget_multiplier, models)
			SELECT $1, g.id, $3, $4, $5, $6, COALESCE($7::numeric, 1), COALESCE($8::text[], '{}')
			FROM groups g WHERE g.id = $2 AND `+sharedChannelGroupEligible+`
			RETURNING id`, []any{in.Name, in.GroupID, in.MaxConcurrency, in.ExpiresAt, in.Published, in.BudgetLimit, in.BudgetMultiplier, pq.Array(in.Models)}, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrSharedChannelGroup
		}
		if err != nil {
			return nil, translateSharedChannelError(err)
		}
	}
	rows, err := client.QueryContext(ctx, `SELECT `+sharedChannelFields+sharedChannelJoin+`WHERE p.id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrSharedChannelNotFound
	}
	item, err := scanSharedChannel(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

func updateSharedPublication(ctx context.Context, client *dbent.Client, id int64, in service.SharedChannelInput) error {
	// Admission and configuration take the same row lock. Reopening an expired
	// publication must end its old grants before new grants can be created.
	var oldGroup int64
	if err := sharedQueryOne(ctx, client, `SELECT group_id FROM shared_channel_publications WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{id}, &oldGroup); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrSharedChannelNotFound
		}
		return err
	}
	var wasLive, budgetExhausted bool
	if err := sharedQueryOne(ctx, client, `SELECT (`+sharedChannelLive+`),
		COALESCE(p.budget_used >= $2::numeric, FALSE)
		`+sharedChannelJoin+`WHERE p.id = $1`, []any{id, in.BudgetLimit}, &wasLive, &budgetExhausted); err != nil {
		return err
	}
	if !wasLive || !in.Published || oldGroup != in.GroupID || budgetExhausted {
		if err := revokeSharedChannelMemberships(ctx, client, id); err != nil {
			return err
		}
	} else {
		var members int
		if err := sharedQueryOne(ctx, client, `SELECT count(*) FROM shared_channel_memberships
			WHERE publication_id = $1 AND `+sharedMembershipActive, []any{id}, &members); err != nil {
			return err
		}
		if members > in.MaxConcurrency {
			return service.ErrSharedChannelFull
		}
	}
	var saved int64
	err := sharedQueryOne(ctx, client, `UPDATE shared_channel_publications p
		SET name = $1, group_id = g.id, max_concurrency = $3, expires_at = $4, published = $5,
			budget_limit = $7, budget_multiplier = COALESCE($8::numeric, 1),
			budget_pricing_blocked = FALSE, models = COALESCE($9::text[], '{}'),
			updated_at = statement_timestamp()
		FROM groups g WHERE p.id = $6 AND g.id = $2
		AND ((`+sharedChannelGroupEligible+`) OR (p.group_id = g.id AND NOT $5)) RETURNING p.id`,
		[]any{in.Name, in.GroupID, in.MaxConcurrency, in.ExpiresAt, in.Published, id, in.BudgetLimit, in.BudgetMultiplier, pq.Array(in.Models)}, &saved)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSharedChannelGroup
	}
	return translateSharedChannelError(err)
}

type sharedChannelExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func revokeSharedChannelMemberships(ctx context.Context, db sharedChannelExecutor, id int64) error {
	_, err := db.ExecContext(ctx, `WITH ended AS (
		UPDATE shared_channel_memberships SET ended_at = statement_timestamp()
		WHERE publication_id = $1 AND ended_at IS NULL RETURNING id
	)
	UPDATE api_keys SET status = 'disabled', updated_at = statement_timestamp()
	WHERE shared_channel_membership_id IN (SELECT id FROM ended) AND deleted_at IS NULL`, id)
	return err
}

func (r *sharedChannelRepository) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var locked int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM shared_channel_publications
		WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSharedChannelNotFound
	}
	if err != nil {
		return err
	}
	if err := revokeSharedChannelMemberships(ctx, tx, id); err != nil {
		return err
	}
	// 保留发布和成员记录，让在途请求仍能结算、历史用量仍能追溯。
	if _, err := tx.ExecContext(ctx, `UPDATE shared_channel_publications
		SET deleted_at = statement_timestamp(), published = FALSE, updated_at = statement_timestamp()
		WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *sharedChannelRepository) WithMembership(ctx context.Context, id, userID int64, fn func(context.Context, service.SharedChannelMembership) error) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	var m service.SharedChannelMembership
	m.PublicationID, m.UserID = id, userID
	var locked int64
	if err := sharedQueryOne(ctx, client, `SELECT id FROM shared_channel_publications WHERE id = $1 FOR UPDATE`, []any{id}, &locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrSharedChannelNotFound
		}
		return err
	}
	if err := sharedQueryOne(ctx, client, `SELECT p.group_id, p.max_concurrency, p.expires_at, p.budget_multiplier, p.budget_limit IS NOT NULL, u.concurrency `+sharedChannelJoin+
		`JOIN users u ON u.id = $2 AND u.deleted_at IS NULL WHERE p.id = $1 AND `+sharedChannelLive,
		[]any{id, userID}, &m.GroupID, &m.MaxConcurrency, &m.ExpiresAt, &m.BudgetMultiplier, &m.BudgetEnabled, &m.PersonalConcurrency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrSharedChannelNotFound
		}
		return err
	}
	if err := sharedQueryOne(ctx, client, `SELECT COALESCE(max(id), 0)
		FROM shared_channel_memberships WHERE publication_id = $1 AND user_id = $2 AND ended_at IS NULL`,
		[]any{id, userID}, &m.ID); err != nil {
		return err
	}
	if m.ID == 0 {
		if err := sharedQueryOne(ctx, client, `INSERT INTO shared_channel_memberships (publication_id, user_id)
			VALUES ($1, $2) RETURNING id`, []any{id, userID}, &m.ID); err != nil {
			return err
		}
	}
	if err := fn(dbent.NewTxContext(ctx, tx), m); err != nil {
		return err
	}
	return tx.Commit()
}

const sharedMembershipFields = `m.id, p.id, p.group_id, m.user_id, p.max_concurrency, p.expires_at, p.budget_multiplier, p.budget_limit IS NOT NULL, u.concurrency`
const sharedMembershipQuery = `SELECT ` + sharedMembershipFields + `, stats.users, stats.rank
	` + sharedChannelJoin + `
	JOIN shared_channel_memberships m ON m.publication_id = p.id
	JOIN users u ON u.id = m.user_id AND u.deleted_at IS NULL
	JOIN LATERAL (
		SELECT count(*) AS users, COALESCE(max(active.rank) FILTER (WHERE active.id = m.id), 0) AS rank
		FROM (
			SELECT id, row_number() OVER (ORDER BY active_since, id) AS rank
			FROM shared_channel_memberships WHERE publication_id = p.id AND ` + sharedMembershipActive + `
		) active
	) stats ON TRUE
	WHERE m.id = $1 AND m.ended_at IS NULL AND ` + sharedChannelLive

func scanSharedMembership(row sharedRow) (*service.SharedChannelMembership, error) {
	var m service.SharedChannelMembership
	err := row.Scan(&m.ID, &m.PublicationID, &m.GroupID, &m.UserID, &m.MaxConcurrency, &m.ExpiresAt,
		&m.BudgetMultiplier, &m.BudgetEnabled, &m.PersonalConcurrency, &m.Members, &m.Rank)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *sharedChannelRepository) FindMembership(ctx context.Context, id int64) (*service.SharedChannelMembership, error) {
	return scanSharedMembership(r.db.QueryRowContext(ctx, sharedMembershipQuery, id))
}

func (r *sharedChannelRepository) ActivateMembership(ctx context.Context, id, userID, groupID int64) (*service.SharedChannelMembership, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var locked int64
	// 与发布配置和额度结算使用同一发布行锁，串行化首次活跃的名额分配。
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM shared_channel_publications p
		JOIN shared_channel_memberships m ON m.publication_id = p.id
		WHERE m.id = $1 AND m.user_id = $2 AND p.group_id = $3 AND m.ended_at IS NULL
		FOR UPDATE OF p`, id, userID, groupID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := scanSharedMembership(tx.QueryRowContext(ctx, sharedMembershipQuery, id))
	if err != nil || m == nil {
		return nil, err
	}
	if m.Rank == 0 && m.Members >= m.MaxConcurrency {
		return nil, service.ErrSharedChannelFull
	}
	_, err = tx.ExecContext(ctx, `UPDATE shared_channel_memberships
		SET active_since = CASE WHEN `+sharedMembershipActive+` THEN active_since ELSE statement_timestamp() END,
			last_active_at = statement_timestamp() WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	m, err = scanSharedMembership(tx.QueryRowContext(ctx, sharedMembershipQuery, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

func (r *sharedChannelRepository) ListMemberships(ctx context.Context, userID int64) ([]service.SharedChannelMembership, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sharedMembershipFields+sharedChannelJoin+`
		JOIN shared_channel_memberships m ON m.publication_id = p.id
		JOIN users u ON u.id = m.user_id AND u.deleted_at IS NULL
		WHERE m.user_id = $1 AND m.ended_at IS NULL AND `+sharedChannelLive, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []service.SharedChannelMembership
	for rows.Next() {
		var m service.SharedChannelMembership
		if err := rows.Scan(&m.ID, &m.PublicationID, &m.GroupID, &m.UserID, &m.MaxConcurrency, &m.ExpiresAt, &m.BudgetMultiplier, &m.BudgetEnabled, &m.PersonalConcurrency); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func translateSharedChannelError(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.Constraint == "shared_channel_publications_group_id_key" {
		return service.ErrSharedChannelGroupBound
	}
	return err
}
