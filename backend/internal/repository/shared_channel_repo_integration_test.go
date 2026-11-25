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
	require.Equal(t, 5, member.Share())
	second := mustCreateUser(t, testEntClient(t), &service.User{Username: "second", Concurrency: 5})
	joinSharedChannelKey(t, repo, p.ID, second.ID)
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
		for _, name := range []string{"253_shared_channel_publications.sql", "254_shared_channel_budget.sql"} {
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
