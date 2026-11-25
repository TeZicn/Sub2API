package service

import (
	"context"
)

func (s *APIKeyService) createSharedChannelKey(ctx context.Context, id, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
	if err := validateCreateAPIKeyRequest(req); err != nil {
		return nil, err
	}
	if s.sharedChannels == nil {
		return nil, ErrSharedChannelNotFound
	}
	var key *APIKey
	err := s.sharedChannels.WithMembership(ctx, id, userID, func(txCtx context.Context, m SharedChannelMembership) error {
		if req.GroupID != nil && *req.GroupID != m.GroupID {
			return ErrSharedChannelNotFound
		}
		req.GroupID = &m.GroupID
		var err error
		key, err = s.mintAPIKey(txCtx, userID, req, &m.ID)
		if err == nil {
			applySharedKeyExpiry(key, m)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	// Invalidate a possible negative lookup only after the transaction commits.
	s.InvalidateAuthCacheByKey(ctx, key.Key)
	return key, nil
}

func (s *APIKeyService) sharedMemberships(ctx context.Context, userID int64) ([]SharedChannelMembership, error) {
	if s.sharedChannels == nil {
		return nil, nil
	}
	return s.sharedChannels.ListMemberships(ctx, userID)
}

func (s *APIKeyService) lookupSharedKeyMembership(ctx context.Context, key *APIKey) (*SharedChannelMembership, error) {
	if s.sharedChannels == nil || key.SharedChannelMembershipID == nil || key.GroupID == nil {
		return nil, nil
	}
	m, err := s.sharedChannels.FindMembership(ctx, *key.SharedChannelMembershipID)
	if err != nil || m == nil {
		return nil, err
	}
	if m.UserID != key.UserID || m.GroupID != *key.GroupID {
		return nil, nil
	}
	return m, nil
}

func (s *APIKeyService) authorizeSharedKey(ctx context.Context, key *APIKey) (*APIKey, error) {
	if key.SharedChannelMembershipID == nil {
		return key, nil
	}
	m, err := s.lookupSharedKeyMembership(ctx, key)
	if err != nil {
		return nil, err
	}
	if m == nil || key.User == nil || key.Group == nil {
		return nil, ErrAPIKeyNotFound
	}
	// Cache entries describe the underlying user, not the privilege of this key.
	user := *key.User
	user.AllowedGroups = append(append([]int64(nil), user.AllowedGroups...), m.GroupID)
	// 鉴权只检查使用资格；实际请求取槽时才激活成员并计算当前份额。
	user.Concurrency = m.PersonalConcurrency
	key.User = &user
	key.SharedChannelBudgetMultiplier = m.BudgetMultiplier
	key.SharedChannelBudgetEnabled = m.BudgetEnabled
	return key, nil
}

func applySharedKeyExpiry(key *APIKey, m SharedChannelMembership) {
	if m.ExpiresAt != nil && (key.ExpiresAt == nil || m.ExpiresAt.Before(*key.ExpiresAt)) {
		key.ExpiresAt = m.ExpiresAt
	}
}

func (s *APIKeyService) decorateSharedKey(ctx context.Context, key *APIKey) error {
	if key.SharedChannelMembershipID == nil {
		return nil
	}
	m, err := s.lookupSharedKeyMembership(ctx, key)
	if err != nil {
		return err
	}
	if m == nil {
		key.Status = StatusAPIKeyDisabled
	} else {
		applySharedKeyExpiry(key, *m)
	}
	return nil
}

func (s *APIKeyService) decorateSharedKeys(ctx context.Context, userID int64, keys []APIKey) error {
	memberships, err := s.sharedMemberships(ctx, userID)
	if err != nil {
		return err
	}
	active := make(map[int64]SharedChannelMembership, len(memberships))
	for _, m := range memberships {
		active[m.ID] = m
	}
	for i := range keys {
		key := &keys[i]
		if key.SharedChannelMembershipID == nil {
			continue
		}
		m, ok := active[*key.SharedChannelMembershipID]
		if !ok || key.GroupID == nil || m.GroupID != *key.GroupID {
			key.Status = StatusAPIKeyDisabled
		} else {
			applySharedKeyExpiry(key, m)
		}
	}
	return nil
}

type sharedSlotContextKey struct{}
type sharedConcurrencyResolverKey struct{}
type sharedConcurrencyResolver func(context.Context, int64) (*SharedChannelMembership, error)

type sharedSlotGrant struct {
	MembershipID int64
	UserID       int64
	GroupID      int64
}

// SharedChannelRequestContext scopes gateway request slots to the key's live
// membership. Queued requests recheck this grant before every allocation attempt.
func (s *APIKeyService) SharedChannelRequestContext(ctx context.Context, key *APIKey) context.Context {
	if key == nil || key.SharedChannelMembershipID == nil || key.GroupID == nil {
		return ctx
	}
	grant := sharedSlotGrant{
		MembershipID: *key.SharedChannelMembershipID,
		UserID:       key.UserID,
		GroupID:      *key.GroupID,
	}
	ctx = context.WithValue(ctx, sharedSlotContextKey{}, grant)
	return context.WithValue(ctx, sharedConcurrencyResolverKey{}, sharedConcurrencyResolver(s.resolveSharedChannelConcurrency))
}

func (s *APIKeyService) resolveSharedChannelConcurrency(ctx context.Context, userID int64) (*SharedChannelMembership, error) {
	grant, ok := ctx.Value(sharedSlotContextKey{}).(sharedSlotGrant)
	if !ok || s.sharedChannels == nil {
		return nil, ErrAPIKeyNotFound
	}
	m, err := s.sharedChannels.ActivateMembership(ctx, grant.MembershipID, grant.UserID, grant.GroupID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.UserID != grant.UserID || m.GroupID != grant.GroupID ||
		userID != grant.UserID || m.Share() < 1 {
		return nil, ErrAPIKeyNotFound
	}
	return m, nil
}

// 每次尝试（包括排队重试）都读取当前成员，三层限制共用现有槽位机制。
func (s *ConcurrencyService) acquireSharedChannelSlots(ctx context.Context, m *SharedChannelMembership) (*AcquireResult, error) {
	limits := []UserWithConcurrency{
		{ID: m.UserID, MaxConcurrency: m.PersonalConcurrency},
		// 用户 ID 为正数；负偶数保留给成员，负奇数保留给共享服务。
		{ID: -2 * m.ID, MaxConcurrency: m.Share()},
		{ID: -2*m.PublicationID - 1, MaxConcurrency: m.MaxConcurrency},
	}
	releases := make([]func(), 0, len(limits))
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, limit := range limits {
		result, err := s.acquireUserSlot(ctx, limit.ID, limit.MaxConcurrency)
		if err != nil || !result.Acquired {
			release()
			return result, err
		}
		releases = append(releases, result.ReleaseFunc)
	}
	return &AcquireResult{Acquired: true, ReleaseFunc: release}, nil
}
