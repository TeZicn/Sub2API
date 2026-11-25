package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

var errSharedChannelPricingUnavailable = errors.New("shared channel budget pricing unavailable")
var errSharedChannelBillingUnavailable = errors.New("shared channel requires deduplicated usage settlement")

func sharedChannelCustomerCost(key *APIKey, cost *CostBreakdown) *CostBreakdown {
	if !key.IsSharedChannel() || cost == nil {
		return cost
	}
	free := *cost
	free.ActualCost = 0
	return &free
}

type sharedChannelCatalogUsage struct {
	Model                  string
	Tokens                 UsageTokens
	ServiceTier            string
	ReasoningEffort        string
	PricingAt              time.Time
	ImageCount             int
	ImageSize              string
	ImageEndpoint          bool
	AudioUsage             *AudioUsage
	VideoCount             int
	VideoResolution        string
	VideoDurationSeconds   int
	WebSearchCalls         int
	SearchCalls            int
	CacheStorageTokenHours float64
}

func calculateSharedChannelCatalogUsageCost(ctx context.Context, billing *BillingService, usage sharedChannelCatalogUsage) (float64, error) {
	if billing == nil {
		return 0, errSharedChannelPricingUnavailable
	}
	model := strings.TrimSpace(usage.Model)
	total := 0.0
	add := func(cost *CostBreakdown) bool {
		return addSharedChannelCatalogCost(&total, cost)
	}

	switch {
	case usage.WebSearchCalls > 0:
		if !add(billing.CalculateWebSearchCost(usage.WebSearchCalls, nil, 1)) {
			return 0, errSharedChannelPricingUnavailable
		}
	case usage.VideoCount > 0:
		if _, ok := getDefaultGrokImagineVideoPrice(model, usage.VideoResolution); !ok {
			return 0, errSharedChannelPricingUnavailable
		}
		if !add(billing.CalculateVideoCost(model, usage.VideoResolution, usage.VideoCount, usage.VideoDurationSeconds, nil, 1)) {
			return 0, errSharedChannelPricingUnavailable
		}
	case usage.AudioUsage != nil:
		switch strings.ToLower(usage.AudioUsage.Mode) {
		case "realtime", "tts", "stt":
		default:
			return 0, errSharedChannelPricingUnavailable
		}
		if !add(billing.CalculateAudioCost(usage.AudioUsage.Mode, usage.AudioUsage.DurationOrUnits, nil, 1)) {
			return 0, errSharedChannelPricingUnavailable
		}
	case usage.ImageEndpoint && usage.ImageCount > 0:
		if !hasSharedChannelImagePrice(billing, model, usage.ImageSize) {
			return 0, errSharedChannelPricingUnavailable
		}
		if !add(billing.CalculateImageCost(model, usage.ImageSize, usage.ImageCount, nil, 1)) {
			return 0, errSharedChannelPricingUnavailable
		}
	default:
		if usageHasBillableTokens(usage.Tokens) {
			tokenCost, err := calculateSharedChannelCatalogTokenCost(
				ctx, billing, model, usage.Tokens, usage.ServiceTier, usage.ReasoningEffort, usage.PricingAt,
			)
			if err != nil || !add(&CostBreakdown{TotalCost: tokenCost}) {
				return 0, errSharedChannelPricingUnavailable
			}
		} else if usage.ImageCount > 0 {
			if !hasSharedChannelImagePrice(billing, model, usage.ImageSize) {
				return 0, errSharedChannelPricingUnavailable
			}
			if !add(billing.CalculateImageCost(model, usage.ImageSize, usage.ImageCount, nil, 1)) {
				return 0, errSharedChannelPricingUnavailable
			}
		}
		if usage.SearchCalls > 0 && !add(billing.CalculateSearchCost(usage.SearchCalls, nil, 1)) {
			return 0, errSharedChannelPricingUnavailable
		}
	}

	if usage.CacheStorageTokenHours > 0 {
		price := billing.CacheStoragePricePerTokenHour(model)
		if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return 0, errSharedChannelPricingUnavailable
		}
		total += usage.CacheStorageTokenHours * price
	}
	if math.IsNaN(total) || math.IsInf(total, 0) || total < 0 {
		return 0, errSharedChannelPricingUnavailable
	}
	return total, nil
}

func (s *GatewayService) sharedChannelBudgetUsageCost(ctx context.Context, key *APIKey, result *ForwardResult, model string, pricingAt time.Time) (float64, bool) {
	if key == nil || key.SharedChannelMembershipID == nil || !key.SharedChannelBudgetEnabled {
		return 0, false
	}
	if result == nil {
		return 0, true
	}
	if upstreamModel := strings.TrimSpace(result.UpstreamModel); upstreamModel != "" {
		model = upstreamModel
	}
	catalogCost, err := calculateSharedChannelCatalogUsageCost(ctx, s.billingService, sharedChannelCatalogUsage{
		Model:                  model,
		Tokens:                 result.Usage.billingUsageTokens(),
		ServiceTier:            optionalStringValue(result.ServiceTier),
		ReasoningEffort:        optionalStringValue(result.ReasoningEffort),
		PricingAt:              pricingAt,
		ImageCount:             result.ImageCount,
		ImageSize:              result.ImageSize,
		ImageEndpoint:          result.ImageEndpoint,
		AudioUsage:             result.AudioUsage,
		SearchCalls:            result.SearchCount,
		CacheStorageTokenHours: result.CacheStorageTokenHours,
	})
	if err != nil {
		return 0, true
	}
	return applySharedChannelBudgetMultiplier(key, catalogCost)
}

func (s *OpenAIGatewayService) sharedChannelBudgetUsageCost(ctx context.Context, key *APIKey, result *OpenAIForwardResult, model string, pricingAt time.Time) (float64, bool) {
	if key == nil || key.SharedChannelMembershipID == nil || !key.SharedChannelBudgetEnabled {
		return 0, false
	}
	if result == nil {
		return 0, true
	}
	if upstreamModel := strings.TrimSpace(result.UpstreamModel); upstreamModel != "" {
		model = upstreamModel
	} else if billingModel := strings.TrimSpace(result.BillingModel); billingModel != "" {
		model = billingModel
	}
	serviceTier := ""
	if result.ServiceTier != nil {
		serviceTier = strings.TrimSpace(*result.ServiceTier)
	}
	catalogCost, err := calculateSharedChannelCatalogUsageCost(ctx, s.billingService, sharedChannelCatalogUsage{
		Model:                model,
		Tokens:               result.Usage.billingUsageTokens(),
		ServiceTier:          serviceTier,
		ReasoningEffort:      optionalStringValue(result.ReasoningEffort),
		PricingAt:            pricingAt,
		ImageCount:           result.ImageCount,
		ImageSize:            result.ImageSize,
		ImageEndpoint:        result.ImageEndpoint,
		AudioUsage:           result.AudioUsage,
		VideoCount:           result.VideoCount,
		VideoResolution:      result.VideoResolution,
		VideoDurationSeconds: result.VideoDurationSeconds,
		WebSearchCalls:       result.WebSearchCalls,
		SearchCalls:          result.SearchCount,
	})
	if err != nil {
		return 0, true
	}
	return applySharedChannelBudgetMultiplier(key, catalogCost)
}

func calculateSharedChannelCatalogTokenCost(
	ctx context.Context,
	billing *BillingService,
	model string,
	tokens UsageTokens,
	serviceTier, reasoningEffort string,
	pricingAt time.Time,
) (float64, error) {
	if billing == nil || strings.TrimSpace(model) == "" {
		return 0, errSharedChannelPricingUnavailable
	}
	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:             ctx,
		Model:           model,
		Tokens:          tokens,
		RateMultiplier:  1,
		PricingAt:       pricingAt,
		ServiceTier:     serviceTier,
		ReasoningEffort: reasoningEffort,
	})
	if err != nil || cost == nil || math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) || cost.TotalCost < 0 {
		return 0, errSharedChannelPricingUnavailable
	}
	return cost.TotalCost, nil
}

// 无官方模型价格时禁止使用通用图片默认价消耗共享额度。
func hasSharedChannelImagePrice(billing *BillingService, model, size string) bool {
	if _, ok := getDefaultGrokImagineImagePrice(model, size); ok {
		return true
	}
	if billing.pricingService == nil {
		return false
	}
	price := billing.pricingService.GetModelPricing(model)
	return price != nil && price.OutputCostPerImage > 0
}

func applySharedChannelBudgetMultiplier(key *APIKey, catalogCost float64) (float64, bool) {
	if math.IsNaN(catalogCost) || math.IsInf(catalogCost, 0) || catalogCost < 0 {
		return 0, true
	}
	multiplier := 1.0
	if key != nil && key.SharedChannelBudgetMultiplier > 0 {
		multiplier = key.SharedChannelBudgetMultiplier
	}
	cost := catalogCost * multiplier
	if math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, true
	}
	return QuantizeUsageBillingAmount(cost), false
}

func usageHasBillableTokens(tokens UsageTokens) bool {
	return tokens.InputTokens != 0 || tokens.ImageInputTokens != 0 || tokens.ImageCacheReadTokens != 0 ||
		tokens.OutputTokens != 0 || tokens.CacheCreationTokens != 0 || tokens.CacheReadTokens != 0 ||
		tokens.CacheCreation5mTokens != 0 || tokens.CacheCreation1hTokens != 0 ||
		tokens.CacheCreation30mTokens != 0 || tokens.CacheCreation12hTokens != 0 ||
		tokens.ImageOutputTokens != 0
}

func addSharedChannelCatalogCost(total *float64, breakdown *CostBreakdown) bool {
	if breakdown == nil || math.IsNaN(breakdown.TotalCost) || math.IsInf(breakdown.TotalCost, 0) || breakdown.TotalCost < 0 {
		return false
	}
	*total += breakdown.TotalCost
	return !math.IsNaN(*total) && !math.IsInf(*total, 0)
}
