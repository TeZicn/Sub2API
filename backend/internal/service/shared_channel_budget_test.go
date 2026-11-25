package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSharedChannelUsageDoesNotChargePersonalBalance(t *testing.T) {
	id := int64(9)
	for _, budget := range []bool{false, true} {
		key := &APIKey{ID: 1, SharedChannelMembershipID: &id, SharedChannelBudgetEnabled: budget}
		original := &CostBreakdown{ActualCost: 5, TotalCost: 2}
		cost := sharedChannelCustomerCost(key, original)
		require.Equal(t, 0.0, cost.ActualCost)
		require.Equal(t, 2.0, cost.TotalCost)
		require.Equal(t, 5.0, original.ActualCost)
		cmd := buildUsageBillingCommand("shared-request", nil, &postUsageBillingParams{
			APIKey: key, User: &User{ID: 1}, Account: &Account{ID: 2}, Cost: original, SharedChannelBudgetCost: 4,
		})
		require.Zero(t, cmd.BalanceCost)
		require.Zero(t, cmd.SubscriptionCost)
		if budget {
			require.Equal(t, 4.0, cmd.SharedChannelBudgetCost)
		} else {
			require.Zero(t, cmd.SharedChannelBudgetCost)
		}
	}
	ordinary := &CostBreakdown{ActualCost: 5, TotalCost: 2}
	require.Same(t, ordinary, sharedChannelCustomerCost(&APIKey{}, ordinary))
}

func TestSharedChannelBillingEligibilityWithoutPersonalBalance(t *testing.T) {
	id := int64(9)
	key := &APIKey{ID: 1, SharedChannelMembershipID: &id}
	s := &BillingCacheService{}
	require.NoError(t, s.CheckBillingEligibility(context.Background(), &User{ID: 1, Balance: 0}, key, &Group{}, nil, PlatformOpenAI))
	reservation, err := s.ReserveFixedCharge(context.Background(), &User{ID: 1}, key, &Group{}, nil, 5)
	require.NoError(t, err)
	require.Nil(t, reservation)
}

func TestSharedChannelBudgetUsesCatalogPriceAndConfiguredMultiplier(t *testing.T) {
	catalogCost, err := calculateSharedChannelCatalogUsageCost(context.Background(), NewBillingService(&config.Config{}, nil), sharedChannelCatalogUsage{
		Model:  "claude-opus-4.5",
		Tokens: UsageTokens{InputTokens: 10_000, OutputTokens: 1_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(catalogCost-0.075) > 1e-12 {
		t.Fatalf("catalog cost = %v, want 0.075 USD", catalogCost)
	}
	got, blocked := applySharedChannelBudgetMultiplier(&APIKey{SharedChannelBudgetMultiplier: 2}, catalogCost)
	if blocked || got != 0.15 {
		t.Fatalf("budget charge = %v, blocked = %t; want 0.15 USD and no block", got, blocked)
	}
}

func TestSharedChannelTimeModeSkipsBudgetPricing(t *testing.T) {
	membershipID := int64(1)
	key := &APIKey{SharedChannelMembershipID: &membershipID}
	gw := &GatewayService{}
	openAI := &OpenAIGatewayService{}
	if cost, blocked := gw.sharedChannelBudgetUsageCost(context.Background(), key, nil, "unknown", time.Now()); cost != 0 || blocked {
		t.Fatal("time-based services must not require budget pricing")
	}
	if cost, blocked := openAI.sharedChannelBudgetUsageCost(context.Background(), key, nil, "unknown", time.Now()); cost != 0 || blocked {
		t.Fatal("time-based OpenAI services must not require budget pricing")
	}
	params := &postUsageBillingParams{APIKey: key, Cost: &CostBreakdown{}, User: &User{}, Account: &Account{}, SharedChannelPricingBlocked: true}
	if cmd := buildUsageBillingCommand("test", nil, params); cmd.SharedChannelMembershipID != 0 || cmd.SharedChannelPricingBlocked {
		t.Fatal("time-based services must not enter budget settlement")
	}
	key.SharedChannelBudgetEnabled = true
	if _, blocked := gw.sharedChannelBudgetUsageCost(context.Background(), key, nil, "unknown", time.Now()); !blocked {
		t.Fatal("budget mode must block unavailable pricing")
	}
}

func TestSharedChannelUnknownMediaPriceDoesNotUseGenericFallback(t *testing.T) {
	for _, usage := range []sharedChannelCatalogUsage{
		{Model: "unknown-image", ImageCount: 1, ImageEndpoint: true},
		{Model: "unknown-video", VideoCount: 1},
	} {
		_, err := calculateSharedChannelCatalogUsageCost(context.Background(), NewBillingService(&config.Config{}, nil), usage)
		if err == nil {
			t.Fatalf("unpriced media model %s must be rejected", usage.Model)
		}
	}
}

func TestSharedChannelBudgetDoesNotChangeOrdinaryBillingFingerprint(t *testing.T) {
	cmd := &UsageBillingCommand{UserID: 1, AccountID: 2, APIKeyID: 3}
	legacy := "1|2|3|||||0|0|0|0|0|0||0|0.0000000000|0.0000000000|0.0000000000|0.0000000000|0.0000000000"
	hash := sha256.Sum256([]byte(legacy))
	if got := buildUsageBillingFingerprint(cmd); got != hex.EncodeToString(hash[:]) {
		t.Fatalf("ordinary request fingerprint changed: %s", got)
	}
	cmd.SharedChannelMembershipID = 4
	before := buildUsageBillingFingerprint(cmd)
	cmd.SharedChannelBudgetCost = 0.1
	if before == buildUsageBillingFingerprint(cmd) {
		t.Fatal("shared budget amount must participate in request deduplication")
	}
}

func TestSharedChannelBudgetStopsWhenCatalogPriceIsUnavailable(t *testing.T) {
	_, err := calculateSharedChannelCatalogUsageCost(context.Background(), NewBillingService(&config.Config{}, nil), sharedChannelCatalogUsage{
		Model:  "unknown-shared-channel-model",
		Tokens: UsageTokens{InputTokens: 1},
	})
	if err == nil {
		t.Fatal("expected unpriced model to block shared channel billing")
	}
}

func TestSharedChannelSaveRequiresAnEndConditionAndFiniteMultiplier(t *testing.T) {
	service := NewSharedChannelService(nil, nil)
	floatPtr := func(value float64) *float64 { return &value }
	limit := 5
	input := SharedChannelInput{
		Name: "pool", GroupID: 1, MaxConcurrency: limit, Published: true,
		BudgetMultiplier: floatPtr(1),
	}
	if _, err := service.Save(context.Background(), 0, input); err == nil {
		t.Fatal("expected a published channel to require an expiry or consumption limit")
	}

	for _, multiplier := range []float64{math.NaN(), math.Inf(1), 0, 1001} {
		input.BudgetLimit = floatPtr(100)
		input.BudgetMultiplier = floatPtr(multiplier)
		if _, err := service.Save(context.Background(), 0, input); err == nil {
			t.Fatalf("expected multiplier %v to be rejected", multiplier)
		}
	}
}
