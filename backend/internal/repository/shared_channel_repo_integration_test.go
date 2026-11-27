//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func sharedChannelFixture(t *testing.T, budget bool) (service.SharedChannelRepository, *service.SharedChannel, *service.User) {
	t.Helper()
	client := testEntClient(t)
	u := mustCreateUser(t, client, &service.User{Username: "shared-user", Balance: 100, Concurrency: 5})
	// 用量写入真实测试库，必须清理自身记录，避免污染随后运行的全局统计测试。
	t.Cleanup(func() {
		for _, query := range []string{"DELETE FROM usage_logs WHERE user_id=$1", "DELETE FROM ops_error_logs WHERE user_id=$1"} {
			_, err := integrationDB.ExecContext(context.Background(), query, u.ID)
			require.NoError(t, err)
		}
	})
	g := mustCreateGroup(t, client, &service.Group{Name: "shared-" + uuid.NewString(), IsExclusive: true, RateMultiplier: 1})
	in := service.SharedChannelInput{Name: "Shared service", GroupID: g.ID, MaxConcurrency: 8, Published: true}
	if budget {
		limit := 100.0
		in.BudgetLimit = &limit
	} else {
		expiry := time.Now().Add(time.Hour)
		in.ExpiresAt = &expiry
	}
	repo := NewSharedChannelRepository(client, integrationDB)
	p, err := repo.Save(context.Background(), 0, in)
	require.NoError(t, err)
	return repo, p, u
}

func joinSharedChannelKey(t *testing.T, repo service.SharedChannelRepository, publicationID, userID int64) (*dbent.APIKey, int64) {
	t.Helper()
	var key *dbent.APIKey
	var membershipID int64
	err := repo.WithMembership(context.Background(), publicationID, userID, func(ctx context.Context, m service.SharedChannelMembership) error {
		membershipID = m.ID
		var err error
		key, err = clientFromContext(ctx, testEntClient(t)).APIKey.Create().
			SetUserID(userID).SetGroupID(m.GroupID).SetSharedChannelMembershipID(m.ID).
			SetKey("sk-shared-test-" + uuid.NewString()).SetName("Shared service").SetStatus(service.StatusActive).Save(ctx)
		return err
	})
	require.NoError(t, err)
	return key, membershipID
}

func TestSharedChannelRepositoryBudgetLifecycle(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batch), func(t *testing.T) {
			ctx := context.Background()
			repo, p, user := sharedChannelFixture(t, true)
			key, memberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
			second, sameMemberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
			require.Equal(t, memberID, sameMemberID, "multiple keys share one membership")
			ordinary := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: user.ID, Key: "sk-ordinary-" + uuid.NewString()})
			billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
			cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID,
				SharedChannelMembershipID: memberID, SharedChannelBudgetCost: 100}
			apply := func() bool {
				if batch {
					out, err := billing.ApplyBatch(ctx, []*service.UsageBillingCommand{cmd})
					require.NoError(t, err)
					return out[0].Result.Applied
				}
				out, err := billing.Apply(ctx, cmd)
				require.NoError(t, err)
				return out.Applied
			}
			require.True(t, apply())
			require.False(t, apply(), "repeated settlement must not consume twice")
			var used, balance float64
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT budget_used FROM shared_channel_publications WHERE id=$1", p.ID).Scan(&used))
			require.Equal(t, 100.0, used)
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
			require.Equal(t, 100.0, balance)
			for _, id := range []int64{key.ID, second.ID, ordinary.ID} {
				var status string
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id=$1", id).Scan(&status))
				if id == ordinary.ID {
					require.Equal(t, service.StatusActive, status)
				} else {
					require.Equal(t, service.StatusAPIKeyDisabled, status)
				}
			}
			member, err := repo.FindMembership(ctx, memberID)
			require.NoError(t, err)
			require.Nil(t, member)
			channels, err := repo.ListPublished(ctx, user.ID)
			require.NoError(t, err)
			for _, channel := range channels {
				require.NotEqual(t, p.ID, channel.ID)
			}
			in := p.SharedChannelInput
			limit := 200.0
			in.BudgetLimit = &limit
			_, err = repo.Save(ctx, p.ID, in)
			require.NoError(t, err)
			member, err = repo.FindMembership(ctx, memberID)
			require.NoError(t, err)
			require.Nil(t, member, "reopening must not revive old grants")
			_, newMemberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
			require.NotEqual(t, memberID, newMemberID)
		})
	}
}

func TestSharedChannelRepositoryTimeAndAllocation(t *testing.T) {
	ctx := context.Background()
	repo, p, user := sharedChannelFixture(t, false)
	_, memberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
	member, err := repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.Zero(t, member.Share(), "creating keys must not occupy active shares")
	member, err = repo.ActivateMembership(ctx, memberID, user.ID, p.GroupID)
	require.NoError(t, err)
	require.Equal(t, 5, member.Share())
	second := mustCreateUser(t, testEntClient(t), &service.User{Username: "second", Concurrency: 5})
	_, secondMemberID := joinSharedChannelKey(t, repo, p.ID, second.ID)
	_, err = repo.ActivateMembership(ctx, secondMemberID, second.ID, p.GroupID)
	require.NoError(t, err)
	member, err = repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.Equal(t, 4, member.Share())
	channels, err := repo.ListPublished(ctx, user.ID)
	require.NoError(t, err)
	var found bool
	for _, channel := range channels {
		if channel.ID == p.ID {
			found = true
			require.Equal(t, 4, channel.Share)
			require.Equal(t, []string{"shared-user", "second"}, channel.Usernames)
			require.Len(t, channel.Hours, 24)
		}
	}
	require.True(t, found)
	_, err = integrationDB.ExecContext(ctx, "UPDATE shared_channel_publications SET expires_at=now()-interval '1 second' WHERE id=$1", p.ID)
	require.NoError(t, err)
	member, err = repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.Nil(t, member)
	err = repo.WithMembership(ctx, p.ID, user.ID, func(context.Context, service.SharedChannelMembership) error {
		t.Fatal("expired service must reject key creation")
		return nil
	})
	require.ErrorIs(t, err, service.ErrSharedChannelNotFound)
	expiry := time.Now().Add(time.Hour)
	in := p.SharedChannelInput
	in.ExpiresAt = &expiry
	_, err = repo.Save(ctx, p.ID, in)
	require.NoError(t, err)
	member, err = repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.Nil(t, member)
}

func TestSharedChannelMigrationsPreserveExistingData(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	// 在回滚事务内复原旧结构，确保测试不会影响共享测试数据库。
	_, err := tx.ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN shared_channel_membership_id;
		DROP TABLE shared_channel_memberships; DROP TABLE shared_channel_publications;`)
	require.NoError(t, err)
	var userID, keyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance,concurrency,username)
		VALUES($1,'preserved-hash',123.45,5,'existing-user') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,status,quota_used)
		VALUES($1,$2,'Existing key','active',12.34) RETURNING id`, userID, uuid.NewString()).Scan(&keyID))
	var beforeUser, beforeKey string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT row_to_json(u)::text FROM users u WHERE id=$1", userID).Scan(&beforeUser))
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT to_jsonb(k)::text FROM api_keys k WHERE id=$1", keyID).Scan(&beforeKey))
	for range 2 {
		for _, name := range []string{"253_shared_channel_publications.sql", "254_shared_channel_budget.sql", "255_shared_channel_activity.sql", "256_shared_channel_deletion.sql", "257_shared_channel_display_models.sql"} {
			body, err := dbmigrations.FS.ReadFile(name)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, string(body))
			require.NoError(t, err)
		}
	}
	var afterUser, afterKey string
	var membership sql.NullInt64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT row_to_json(u)::text FROM users u WHERE id=$1", userID).Scan(&afterUser))
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT (to_jsonb(k)-'shared_channel_membership_id')::text, shared_channel_membership_id FROM api_keys k WHERE id=$1", keyID).Scan(&afterKey, &membership))
	require.JSONEq(t, beforeUser, afterUser)
	require.JSONEq(t, beforeKey, afterKey)
	require.False(t, membership.Valid)
}

func TestSharedChannelActivityExpiresWithoutRevokingKeys(t *testing.T) {
	ctx := context.Background()
	repo, p, first := sharedChannelFixture(t, false)
	firstKey, firstID := joinSharedChannelKey(t, repo, p.ID, first.ID)
	_, sameID := joinSharedChannelKey(t, repo, p.ID, first.ID)
	require.Equal(t, firstID, sameID)
	second := mustCreateUser(t, testEntClient(t), &service.User{Username: "active-second", Concurrency: 5})
	_, secondID := joinSharedChannelKey(t, repo, p.ID, second.ID)
	assertUsers := func(expected []string) {
		t.Helper()
		channels, err := repo.ListPublished(ctx, first.ID)
		require.NoError(t, err)
		for _, channel := range channels {
			if channel.ID == p.ID {
				require.Equal(t, expected, channel.Usernames)
				require.True(t, channel.Joined, "idle users keep their existing keys and membership")
				return
			}
		}
		t.Fatal("publication missing")
	}
	assertUsers([]string{})
	member, err := repo.ActivateMembership(ctx, firstID, first.ID, p.GroupID)
	require.NoError(t, err)
	require.Equal(t, 5, member.Share())
	_, err = repo.ActivateMembership(ctx, sameID, first.ID, p.GroupID)
	require.NoError(t, err)
	assertUsers([]string{first.Username})
	member, err = repo.ActivateMembership(ctx, secondID, second.ID, p.GroupID)
	require.NoError(t, err)
	require.Equal(t, 4, member.Share())
	assertUsers([]string{first.Username, second.Username})

	_, err = integrationDB.ExecContext(ctx, `UPDATE shared_channel_memberships
		SET last_active_at = statement_timestamp() - interval '90 seconds' WHERE id = $1`, firstID)
	require.NoError(t, err)
	assertUsers([]string{first.Username, second.Username})
	member, err = repo.FindMembership(ctx, firstID)
	require.NoError(t, err)
	require.Equal(t, 4, member.Share(), "a user active within two minutes keeps their share")

	_, err = integrationDB.ExecContext(ctx, `UPDATE shared_channel_memberships
		SET last_active_at = statement_timestamp() - interval '2 minutes 1 second' WHERE id = $1`, firstID)
	require.NoError(t, err)
	assertUsers([]string{second.Username})
	member, err = repo.FindMembership(ctx, secondID)
	require.NoError(t, err)
	require.Equal(t, 5, member.Share(), "remaining user regains the personal cap")
	member, err = repo.FindMembership(ctx, firstID)
	require.NoError(t, err)
	require.NotNil(t, member)
	require.Zero(t, member.Share())
	key, err := testEntClient(t).APIKey.Get(ctx, firstKey.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, key.Status)

	member, err = repo.ActivateMembership(ctx, firstID, first.ID, p.GroupID)
	require.NoError(t, err)
	require.Equal(t, 4, member.Share())
	require.Equal(t, 2, member.Rank, "returning users rejoin at the end of the active order")
	assertUsers([]string{second.Username, first.Username})
	var before, after time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT last_active_at FROM shared_channel_memberships WHERE id=$1`, firstID).Scan(&before))
	_, err = repo.ActivateMembership(ctx, firstID, second.ID, p.GroupID)
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT last_active_at FROM shared_channel_memberships WHERE id=$1`, firstID).Scan(&after))
	require.Equal(t, before, after, "a mismatched user cannot refresh activity")
}

func TestSharedChannelConcurrentActivityAdmission(t *testing.T) {
	ctx := context.Background()
	repo, p, first := sharedChannelFixture(t, false)
	in := p.SharedChannelInput
	in.MaxConcurrency = 1
	_, err := repo.Save(ctx, p.ID, in)
	require.NoError(t, err)
	second := mustCreateUser(t, testEntClient(t), &service.User{Username: "waiting-user", Concurrency: 5})
	_, firstID := joinSharedChannelKey(t, repo, p.ID, first.ID)
	_, secondID := joinSharedChannelKey(t, repo, p.ID, second.ID)
	type admission struct {
		id, userID int64
		err        error
	}
	start := make(chan struct{})
	results := make(chan admission, 2)
	for _, item := range []admission{{id: firstID, userID: first.ID}, {id: secondID, userID: second.ID}} {
		go func(item admission) {
			<-start
			_, item.err = repo.ActivateMembership(ctx, item.id, item.userID, p.GroupID)
			results <- item
		}(item)
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil {
		a, b = b, a
	}
	require.NoError(t, a.err)
	require.ErrorIs(t, b.err, service.ErrSharedChannelFull)
	_, err = integrationDB.ExecContext(ctx, `UPDATE shared_channel_memberships
		SET last_active_at = statement_timestamp() - interval '2 minutes 1 second' WHERE id = $1`, a.id)
	require.NoError(t, err)
	member, err := repo.ActivateMembership(ctx, b.id, b.userID, p.GroupID)
	require.NoError(t, err)
	require.Equal(t, 1, member.Share())
}

func TestSharedChannelRepositoryRejectsBothEndConditions(t *testing.T) {
	repo, p, _ := sharedChannelFixture(t, false)
	in := p.SharedChannelInput
	limit := 100.0
	in.BudgetLimit = &limit
	_, err := repo.Save(context.Background(), p.ID, in)
	require.ErrorContains(t, err, "shared_channel_single_end_condition")
}

func TestSharedChannelTimeModeIgnoresPendingBudgetPricingFailure(t *testing.T) {
	ctx := context.Background()
	repo, p, user := sharedChannelFixture(t, true)
	key, memberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
	in := p.SharedChannelInput
	in.BudgetLimit = nil
	expiry := time.Now().Add(time.Hour)
	in.ExpiresAt = &expiry
	_, err := repo.Save(ctx, p.ID, in)
	require.NoError(t, err)
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err = billing.Apply(ctx, &service.UsageBillingCommand{
		RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID,
		SharedChannelMembershipID: memberID, SharedChannelPricingBlocked: true,
	})
	require.NoError(t, err)
	member, err := repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.NotNil(t, member, "an earlier budget request cannot pause a time-based service")
}

func TestSharedChannelDeletePreservesHistoryAndAllowsGroupReuse(t *testing.T) {
	ctx := context.Background()
	repo, p, user := sharedChannelFixture(t, true)
	key, memberID := joinSharedChannelKey(t, repo, p.ID, user.ID)
	secondKey, _ := joinSharedChannelKey(t, repo, p.ID, user.ID)
	ordinary := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: user.ID, Key: "sk-ordinary-" + uuid.NewString()})
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "shared-history-" + uuid.NewString()})
	usage := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "public-model", InputTokens: 10, TotalCost: 1, ActualCost: 1}
	usageRepo := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB)
	_, err := usageRepo.Create(ctx, usage)
	require.NoError(t, err)
	billing := NewUsageBillingRepository(testEntClient(t), integrationDB)
	cmd := &service.UsageBillingCommand{RequestID: usage.RequestID, UserID: user.ID, APIKeyID: key.ID,
		SharedChannelMembershipID: memberID, SharedChannelBudgetCost: 1}
	_, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, p.ID))
	require.ErrorIs(t, repo.Delete(ctx, p.ID), service.ErrSharedChannelNotFound)
	for _, id := range []int64{key.ID, secondKey.ID, ordinary.ID} {
		var status string
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id=$1", id).Scan(&status))
		if id == ordinary.ID {
			require.Equal(t, service.StatusActive, status)
		} else {
			require.Equal(t, service.StatusAPIKeyDisabled, status)
		}
	}
	member, err := repo.FindMembership(ctx, memberID)
	require.NoError(t, err)
	require.Nil(t, member)
	member, err = repo.ActivateMembership(ctx, memberID, user.ID, p.GroupID)
	require.NoError(t, err)
	require.Nil(t, member)
	items, err := repo.List(ctx)
	require.NoError(t, err)
	for _, item := range items {
		require.NotEqual(t, p.ID, item.ID)
	}
	published, err := repo.ListPublished(ctx, user.ID)
	require.NoError(t, err)
	for _, item := range published {
		require.NotEqual(t, p.ID, item.ID)
	}
	_, err = repo.Save(ctx, p.ID, p.SharedChannelInput)
	require.ErrorIs(t, err, service.ErrSharedChannelNotFound)
	err = repo.WithMembership(ctx, p.ID, user.ID, func(context.Context, service.SharedChannelMembership) error {
		t.Fatal("deleted publication must not issue keys")
		return nil
	})
	require.ErrorIs(t, err, service.ErrSharedChannelNotFound)
	replacement, err := repo.Save(ctx, 0, p.SharedChannelInput)
	require.NoError(t, err)
	require.NotEqual(t, p.ID, replacement.ID)
	_, newMember := joinSharedChannelKey(t, repo, replacement.ID, user.ID)
	require.NotEqual(t, memberID, newMember)
	// Deletion must not break late settlement or recreate old grants.
	cmd.RequestID = uuid.NewString()
	_, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	var used, replacementUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT budget_used FROM shared_channel_publications WHERE id=$1", p.ID).Scan(&used))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT budget_used FROM shared_channel_publications WHERE id=$1", replacement.ID).Scan(&replacementUsed))
	require.Equal(t, 2.0, used)
	require.Zero(t, replacementUsed)
	var logs, bills int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM usage_logs WHERE api_key_id=$1", key.ID).Scan(&logs))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM usage_billing_dedup WHERE api_key_id=$1", key.ID).Scan(&bills))
	require.Equal(t, 1, logs)
	require.Equal(t, 2, bills)
}

func TestSharedChannelCustomModelsRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, p, user := sharedChannelFixture(t, false)
	input := p.SharedChannelInput
	input.Models = []string{"Custom display model", "model-two"}
	saved, err := repo.Save(ctx, p.ID, input)
	require.NoError(t, err)
	require.Equal(t, input.Models, saved.Models)
	published, err := repo.ListPublished(ctx, user.ID)
	require.NoError(t, err)
	found := false
	for _, item := range published {
		if item.ID == p.ID {
			found = true
			require.Equal(t, input.Models, item.Models)
		}
	}
	require.True(t, found)
	input.Models = []string{}
	saved, err = repo.Save(ctx, p.ID, input)
	require.NoError(t, err)
	require.Empty(t, saved.Models)
	require.NotNil(t, saved.Models)
}

func TestSharedChannelStatusCountsOnlyFinalProviderFailures(t *testing.T) {
	ctx := context.Background()
	repo, publication, user := sharedChannelFixture(t, false)
	key, _ := joinSharedChannelKey(t, repo, publication.ID, user.ID)
	ops := NewOpsRepository(integrationDB)
	// 固定在前一小时内部，避免跨整点时测试结果发生漂移。
	var bucket time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT date_trunc('hour', statement_timestamp()) - interval '1 hour'`).Scan(&bucket))
	for _, sample := range []struct {
		name, phase, owner            string
		status                        int
		business, counted             bool
		failure, rateLimit, recovered bool
	}{
		{"upstream 500", "upstream", "provider", 500, false, true, true, false, false},
		{"upstream 429", "upstream", "provider", 429, false, true, false, true, false},
		{"upstream auth", "upstream", "provider", 401, false, true, true, false, false},
		{"account token failure", "account_auth", "provider", 502, false, true, true, false, false},
		{"transport timeout", "network", "provider", 504, false, true, true, false, false},
		{"stream interrupted", "upstream", "provider", 502, false, true, true, false, false},
		{"invalid client request", "request", "client", 400, false, false, false, false, false},
		{"invalid client key", "auth", "client", 401, true, false, false, false, false},
		{"local limit", "concurrency", "platform", 429, true, false, false, false, false},
		{"local quota", "billing", "platform", 403, true, false, false, false, false},
		{"no available accounts", "routing", "platform", 503, true, false, false, false, false},
		{"internal error", "internal", "platform", 500, false, false, false, false, false},
		{"recovered retry limit", "upstream", "provider", 200, false, true, false, true, true},
		{"client cancelled", "network", "provider", 499, false, false, false, false, false},
		{"request policy", "request", "provider", 400, true, false, false, false, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			requestID := uuid.NewString()
			if sample.recovered {
				account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "shared-recovered-" + uuid.NewString()})
				_, err := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB).Create(ctx, &service.UsageLog{
					UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID,
					Model: "test-model", InputTokens: 1, CreatedAt: bucket.Add(time.Minute),
				})
				require.NoError(t, err)
			}
			var upstreamErrorsJSON *string
			if sample.recovered {
				upstreamErrors := `[{"upstream_status_code":429,"kind":"http_error"}]`
				upstreamErrorsJSON = &upstreamErrors
			}
			id, err := ops.InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
				RequestID: requestID, APIKeyID: &key.ID, UserID: &user.ID,
				ErrorPhase: sample.phase, ErrorOwner: sample.owner, ErrorType: "api_error", Severity: "P2",
				StatusCode: sample.status, IsBusinessLimited: sample.business, CreatedAt: bucket.Add(time.Minute),
				UpstreamErrorsJSON: upstreamErrorsJSON,
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE id=$1", id)
				require.NoError(t, err)
			})
			channels, err := repo.ListPublished(ctx, user.ID)
			require.NoError(t, err)
			for _, channel := range channels {
				if channel.ID != publication.ID {
					continue
				}
				require.Len(t, channel.Hours, 24)
				for _, hour := range channel.Hours {
					wantRequests, wantFailures, wantRateLimits := 0, 0, 0
					if hour.StartedAt.Equal(bucket) && sample.counted {
						wantRequests = 1
						if sample.failure {
							wantFailures = 1
						}
						if sample.rateLimit {
							wantRateLimits = 1
						}
					}
					require.Equal(t, wantFailures, hour.Failures)
					require.Equal(t, wantRequests, hour.Requests)
					require.Equal(t, wantRateLimits, hour.RateLimits)
					if sample.counted && sample.rateLimit {
						require.Equal(t, service.SharedChannelHealthDegraded, hour.Status)
					}
				}
				return
			}
			t.Fatal("publication missing")
		})
	}
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "shared-status-" + uuid.NewString()})
	_, err := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB).Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(),
		Model: "test-model", InputTokens: 1, CreatedAt: bucket.Add(time.Minute),
	})
	require.NoError(t, err)
	channels, err := repo.ListPublished(ctx, user.ID)
	require.NoError(t, err)
	found := false
	for _, channel := range channels {
		if channel.ID == publication.ID {
			for _, hour := range channel.Hours {
				if hour.StartedAt.Equal(bucket) {
					found = true
					require.Equal(t, 1, hour.Requests)
					require.Zero(t, hour.Failures)
				}
			}
		}
	}
	require.True(t, found, "successful upstream request must appear in its hour")
}

func TestSharedChannelRecentHealthDeduplicatesRecoveredThrottle(t *testing.T) {
	ctx := context.Background()
	repo, publication, user := sharedChannelFixture(t, false)
	key, _ := joinSharedChannelKey(t, repo, publication.ID, user.ID)
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "shared-recent-" + uuid.NewString()})
	requestID := uuid.NewString()
	var createdAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT statement_timestamp()`).Scan(&createdAt))
	_, err := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB).Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: requestID,
		Model: "test-model", InputTokens: 1, CreatedAt: createdAt,
	})
	require.NoError(t, err)
	upstreamErrors := `[{"upstream_status_code":429,"kind":"http_error"}]`
	_, err = NewOpsRepository(integrationDB).InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
		RequestID: requestID, APIKeyID: &key.ID, UserID: &user.ID,
		ErrorPhase: "upstream", ErrorOwner: "provider", ErrorType: "api_error", Severity: "P2",
		StatusCode: 200, CreatedAt: createdAt, UpstreamErrorsJSON: &upstreamErrors,
	})
	require.NoError(t, err)

	channels, err := repo.ListPublished(ctx, user.ID)
	require.NoError(t, err)
	for _, channel := range channels {
		if channel.ID != publication.ID {
			continue
		}
		require.Equal(t, service.SharedChannelHealth{
			Requests: 1, Failures: 0, RateLimits: 1, Status: service.SharedChannelHealthDegraded,
		}, channel.Recent)
		return
	}
	t.Fatal("publication missing")
}

func TestSharedChannelParticipantNameUsesEmailPrefixWhenUsernameBlank(t *testing.T) {
	ctx := context.Background()
	repo, publication, user := sharedChannelFixture(t, false)
	_, memberID := joinSharedChannelKey(t, repo, publication.ID, user.ID)
	_, err := repo.ActivateMembership(ctx, memberID, user.ID, publication.GroupID)
	require.NoError(t, err)
	prefix := "alice-" + uuid.NewString()
	for _, sample := range []struct{ username, want string }{
		{"Alice", "Alice"}, {"  Alice  ", "Alice"}, {"", prefix}, {"   ", prefix},
	} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE users SET username=$1,email=$2 WHERE id=$3`, sample.username, prefix+"@example.com", user.ID)
		require.NoError(t, err)
		channels, err := repo.ListPublished(ctx, user.ID)
		require.NoError(t, err)
		found := false
		for _, channel := range channels {
			if channel.ID == publication.ID {
				found = true
				require.Equal(t, []string{sample.want}, channel.Usernames)
				require.NotContains(t, channel.Usernames[0], "@example.com")
			}
		}
		require.True(t, found)
	}
}
