//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type sharedAccessGroupRepo struct {
	visibilityGroupRepo
}

func (r *sharedAccessGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	for _, group := range r.groups {
		if group.ID == id {
			return &group, nil
		}
	}
	return nil, errors.New("group not found")
}

type sharedAccessMembershipRepo struct {
	SharedChannelRepository
	member SharedChannelMembership
}

func (r *sharedAccessMembershipRepo) ListMemberships(context.Context, int64) ([]SharedChannelMembership, error) {
	return []SharedChannelMembership{r.member}, nil
}

func (r *sharedAccessMembershipRepo) WithMembership(context.Context, int64, int64, func(context.Context, SharedChannelMembership) error) error {
	return errors.New("ordinary key creation reached shared key minting")
}

func TestSharedChannelMembershipDoesNotGrantOrdinaryGroupAccess(t *testing.T) {
	// 成员资格可以在最后一把 Key 删除后继续存在，但不能变成普通分组授权。
	svc := &APIKeyService{
		userRepo:    &visibilityUserRepo{user: &User{ID: 1, AllowedGroups: []int64{3}}},
		userSubRepo: &visibilitySubRepo{},
		groupRepo: &sharedAccessGroupRepo{visibilityGroupRepo{groups: []Group{
			{ID: 2, IsExclusive: true}, {ID: 3, IsExclusive: true}, {ID: 4},
		}}},
		sharedChannels: &sharedAccessMembershipRepo{member: SharedChannelMembership{ID: 5, PublicationID: 6, GroupID: 2, UserID: 1}},
	}
	t.Run("picker only includes independently authorized groups", func(t *testing.T) {
		groups, err := svc.GetAvailableGroups(context.Background(), 1)
		require.NoError(t, err)
		var ids []int64
		for _, group := range groups {
			ids = append(ids, group.ID)
		}
		require.Equal(t, []int64{3, 4}, ids)
	})
	t.Run("direct create cannot reuse shared membership", func(t *testing.T) {
		groupID := int64(2)
		key, err := svc.Create(context.Background(), 1, CreateAPIKeyRequest{Name: "ordinary", GroupID: &groupID})
		require.ErrorIs(t, err, ErrGroupNotAllowed)
		require.Nil(t, key)
	})
}
