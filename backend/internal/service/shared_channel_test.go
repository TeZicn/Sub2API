package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSharedChannelMembershipShare(t *testing.T) {
	tests := []struct {
		name    string
		maximum int
		members int
		rank    int
		want    int
	}{
		{name: "first user receives remainder", maximum: 5, members: 2, rank: 1, want: 3},
		{name: "second user receives floor share", maximum: 5, members: 2, rank: 2, want: 2},
		{name: "five users divide eight requests", maximum: 8, members: 5, rank: 1, want: 2},
		{name: "last user receives floor share", maximum: 8, members: 5, rank: 5, want: 1},
		{name: "no active members", maximum: 5, members: 0, rank: 0, want: 0},
		{name: "invalid rank", maximum: 5, members: 2, rank: 3, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (SharedChannelMembership{
				MaxConcurrency: tt.maximum,
				Members:        tt.members,
				Rank:           tt.rank,
			}).Share()
			if got != tt.want {
				t.Fatalf("Share() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSharedChannelPersonalCapAndEvenShares(t *testing.T) {
	for _, tt := range []struct {
		members int
		want    []int
	}{
		{1, []int{5}}, {2, []int{4, 4}}, {3, []int{3, 3, 2}},
		{8, []int{1, 1, 1, 1, 1, 1, 1, 1}},
	} {
		for rank, want := range tt.want {
			m := SharedChannelMembership{MaxConcurrency: 8, Members: tt.members, Rank: rank + 1, PersonalConcurrency: 5}
			require.Equal(t, want, m.Share())
		}
	}
}

type sharedChannelMembershipStub struct {
	SharedChannelRepository
	membership  *SharedChannelMembership
	activateErr error
}

func (r *sharedChannelMembershipStub) FindMembership(context.Context, int64) (*SharedChannelMembership, error) {
	return r.membership, nil
}

func (r *sharedChannelMembershipStub) ActivateMembership(context.Context, int64, int64, int64) (*SharedChannelMembership, error) {
	return r.membership, r.activateErr
}

func TestSharedChannelIdleKeyRemainsAuthorized(t *testing.T) {
	m := &SharedChannelMembership{ID: 1, GroupID: 2, UserID: 3, PersonalConcurrency: 5, MaxConcurrency: 8}
	svc := &APIKeyService{sharedChannels: &sharedChannelMembershipStub{membership: m}}
	key, err := svc.authorizeSharedKey(context.Background(), &APIKey{
		UserID: m.UserID, GroupID: &m.GroupID, SharedChannelMembershipID: &m.ID,
		User: &User{Concurrency: 5}, Group: &Group{ID: m.GroupID},
	})
	require.NoError(t, err)
	require.Equal(t, 5, key.User.Concurrency)
	require.Contains(t, key.User.AllowedGroups, m.GroupID)
}

type sharedChannelSlotCache struct {
	ConcurrencyCache
	active map[int64]int
	failID int64
}

func (c *sharedChannelSlotCache) AcquireUserSlot(_ context.Context, id int64, limit int, _ string) (bool, error) {
	if id == c.failID {
		return false, errors.New("slot store unavailable")
	}
	if c.active[id] >= limit {
		return false, nil
	}
	c.active[id]++
	return true, nil
}

func (c *sharedChannelSlotCache) ReleaseUserSlot(_ context.Context, id int64, _ string) error {
	c.active[id]--
	return nil
}

func TestSharedChannelSlotsRespectPersonalPoolAndLiveMemberLimits(t *testing.T) {
	m := &SharedChannelMembership{ID: 10, PublicationID: 20, GroupID: 30, UserID: 40, MaxConcurrency: 8, Members: 1, Rank: 1, PersonalConcurrency: 5}
	repo := &sharedChannelMembershipStub{membership: m}
	keys := &APIKeyService{sharedChannels: repo}
	cache := &sharedChannelSlotCache{active: make(map[int64]int)}
	svc := &ConcurrencyService{cache: cache}
	key := &APIKey{UserID: m.UserID, GroupID: &m.GroupID, SharedChannelMembershipID: &m.ID, User: &User{Concurrency: 5}, Group: &Group{ID: m.GroupID}}
	authorized, err := keys.authorizeSharedKey(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, 5, authorized.User.Concurrency)
	ctx := keys.SharedChannelRequestContext(context.Background(), key)
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	acquire := func() *AcquireResult {
		result, err := svc.AcquireUserSlot(ctx, m.UserID, 8)
		require.NoError(t, err)
		if result.Acquired {
			releases = append(releases, result.ReleaseFunc)
		}
		return result
	}
	for i := 0; i < 5; i++ {
		require.True(t, acquire().Acquired)
	}
	require.False(t, acquire().Acquired, "a shared key cannot bypass the personal limit")
	ordinary, err := svc.AcquireUserSlot(context.Background(), m.UserID, 5)
	require.NoError(t, err)
	require.False(t, ordinary.Acquired, "ordinary and shared keys share the personal cap")

	releases[len(releases)-1]()
	releases = releases[:len(releases)-1]
	m.Members = 2
	require.False(t, acquire().Acquired, "joining reduces the live share to four")
	require.Equal(t, 4, cache.active[m.UserID], "rejected member allocation releases the personal slot")
	m.Members = 1
	require.True(t, acquire().Acquired, "a larger live share is available without a new auth context")

	releases[len(releases)-1]()
	releases = releases[:len(releases)-1]
	poolSlot := -2*m.PublicationID - 1
	cache.active[poolSlot] = 8
	require.False(t, acquire().Acquired, "in-flight traffic cannot overflow the pool after rebalancing")
	require.Equal(t, 4, cache.active[m.UserID])
	require.Equal(t, 4, cache.active[-2*m.ID])
	cache.active[poolSlot] = 4
	cache.failID = poolSlot
	_, err = svc.AcquireUserSlot(ctx, m.UserID, 8)
	require.Error(t, err)
	require.Equal(t, 4, cache.active[m.UserID])
	require.Equal(t, 4, cache.active[-2*m.ID])
	cache.failID = 0
	repo.activateErr = ErrSharedChannelFull
	result, err := svc.AcquireUserSlot(ctx, m.UserID, 8)
	require.NoError(t, err, "a full active pool uses the existing wait/rate-limit path")
	require.False(t, result.Acquired)
	require.Equal(t, 4, cache.active[m.UserID])
	repo.activateErr = nil

	repo.membership = nil
	_, err = svc.AcquireUserSlot(ctx, m.UserID, 8)
	require.ErrorIs(t, err, ErrAPIKeyNotFound, "ended services reject queued requests")
}

func TestSharedChannelRejectsOverlappingEndModes(t *testing.T) {
	expiry, budget := time.Now().Add(time.Hour), 100.0
	svc := NewSharedChannelService(nil, nil)
	_, err := svc.Save(context.Background(), 0, SharedChannelInput{
		Name: "test", GroupID: 1, MaxConcurrency: 8, Published: true, ExpiresAt: &expiry, BudgetLimit: &budget,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "either time-based or budget-based")
}

type sharedChannelSaveStub struct {
	SharedChannelRepository
	input SharedChannelInput
}

func (r *sharedChannelSaveStub) Save(_ context.Context, _ int64, input SharedChannelInput) (*SharedChannel, error) {
	r.input = input
	return &SharedChannel{SharedChannelInput: input}, nil
}

func TestSharedChannelCustomModelsValidation(t *testing.T) {
	repo := &sharedChannelSaveStub{}
	svc := NewSharedChannelService(repo, nil)
	input := SharedChannelInput{Name: "Service", GroupID: 1, MaxConcurrency: 5,
		Models: []string{" model-a ", "", "model-a", "Display name"}}
	_, err := svc.Save(context.Background(), 0, input)
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "Display name"}, repo.input.Models)
	for _, models := range [][]string{{strings.Repeat("a", 201)}, {"bad\x00model"}, make([]string, 101)} {
		input.Models = models
		_, err := svc.Save(context.Background(), 0, input)
		require.Error(t, err)
	}
	input.Models = nil
	_, err = svc.Save(context.Background(), 0, input)
	require.NoError(t, err)
	require.NotNil(t, repo.input.Models)
	require.Empty(t, repo.input.Models)
}
