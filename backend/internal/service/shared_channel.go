package service

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSharedChannelGroup      = infraerrors.Conflict("SHARED_CHANNEL_GROUP_INVALID", "Choose an active exclusive standard group")
	ErrSharedChannelGroupBound = infraerrors.Conflict("SHARED_CHANNEL_GROUP_BOUND", "This group already has a shared channel")
	ErrSharedChannelNotFound   = infraerrors.NotFound("SHARED_CHANNEL_NOT_FOUND", "Shared channel not found or no longer published")
	ErrSharedChannelFull       = infraerrors.Conflict("SHARED_CHANNEL_FULL", "The shared channel has no more places")
	ErrSharedChannelKeyGroup   = infraerrors.BadRequest("SHARED_CHANNEL_KEY_GROUP_FIXED", "A shared channel key cannot switch groups")
	ErrSharedChannelKeyEnded   = infraerrors.Conflict("SHARED_CHANNEL_KEY_ENDED", "The shared channel key has expired or been withdrawn")
)

type SharedChannelInput struct {
	Name             string     `json:"name"`
	Models           []string   `json:"models"`
	GroupID          int64      `json:"group_id"`
	MaxConcurrency   int        `json:"max_concurrency"`
	ExpiresAt        *time.Time `json:"expires_at"`
	Published        bool       `json:"published"`
	BudgetLimit      *float64   `json:"budget_limit"`
	BudgetMultiplier *float64   `json:"budget_multiplier"`
}

type SharedChannel struct {
	SharedChannelInput
	ID                   int64   `json:"id"`
	GroupName            string  `json:"group_name"`
	GroupEligible        bool    `json:"group_eligible"`
	Platform             string  `json:"platform"`
	BudgetUsed           float64 `json:"budget_used"`
	BudgetPricingBlocked bool    `json:"budget_pricing_blocked"`
}

type SharedChannelHour struct {
	StartedAt time.Time `json:"started_at"`
	Requests  int       `json:"requests"`
	Failures  int       `json:"failures"`
}

// PublishedSharedChannel does not expose private group details or member IDs.
type PublishedSharedChannel struct {
	GroupID        int64               `json:"-"`
	ID             int64               `json:"id"`
	Name           string              `json:"name"`
	Platform       string              `json:"platform"`
	Models         []string            `json:"models"`
	MaxConcurrency int                 `json:"max_concurrency"`
	ExpiresAt      *time.Time          `json:"expires_at"`
	BudgetLimit    *float64            `json:"budget_limit"`
	BudgetUsed     float64             `json:"budget_used"`
	Usernames      []string            `json:"usernames"`
	Joined         bool                `json:"joined"`
	Share          int                 `json:"share"`
	Hours          []SharedChannelHour `json:"hours"`
}

type SharedChannelMembership struct {
	ID, PublicationID, GroupID, UserID int64
	MaxConcurrency, Members, Rank      int
	ExpiresAt                          *time.Time
	BudgetMultiplier                   float64
	BudgetEnabled                      bool
	PersonalConcurrency                int
}

func (m SharedChannelMembership) Share() int {
	if m.Members < 1 || m.Rank < 1 || m.Rank > m.Members {
		return 0
	}
	share := m.MaxConcurrency / m.Members
	if m.Rank <= m.MaxConcurrency%m.Members {
		share++
	}
	if m.PersonalConcurrency > 0 {
		share = min(share, m.PersonalConcurrency)
	}
	return share
}

type SharedChannelRepository interface {
	List(context.Context) ([]SharedChannel, error)
	ListPublished(context.Context, int64) ([]PublishedSharedChannel, error)
	Save(context.Context, int64, SharedChannelInput) (*SharedChannel, error)
	Delete(context.Context, int64) error
	WithMembership(context.Context, int64, int64, func(context.Context, SharedChannelMembership) error) error
	FindMembership(context.Context, int64) (*SharedChannelMembership, error)
	ActivateMembership(context.Context, int64, int64, int64) (*SharedChannelMembership, error)
	ListMemberships(context.Context, int64) ([]SharedChannelMembership, error)
}

type SharedChannelService struct {
	repo SharedChannelRepository
	keys *APIKeyService
}

func NewSharedChannelService(repo SharedChannelRepository, keys *APIKeyService) *SharedChannelService {
	return &SharedChannelService{repo: repo, keys: keys}
}

func (s *SharedChannelService) List(ctx context.Context) ([]SharedChannel, error) {
	return s.repo.List(ctx)
}

func (s *SharedChannelService) ListPublished(ctx context.Context, userID int64) ([]PublishedSharedChannel, error) {
	return s.repo.ListPublished(ctx, userID)
}

func (s *SharedChannelService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *SharedChannelService) Save(ctx context.Context, id int64, input SharedChannelInput) (*SharedChannel, error) {
	input.Name = strings.TrimSpace(input.Name)
	models, err := normalizeSharedChannelModels(input.Models)
	if err != nil {
		return nil, err
	}
	input.Models = models
	if input.BudgetMultiplier == nil {
		defaultMultiplier := 1.0
		input.BudgetMultiplier = &defaultMultiplier
	}
	switch {
	case input.Name == "" || utf8.RuneCountInString(input.Name) > 100:
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_NAME_INVALID", "Channel name must be 1 to 100 characters")
	case input.GroupID <= 0:
		return nil, ErrSharedChannelGroup
	case input.MaxConcurrency < 1 || input.MaxConcurrency > 1000:
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_LIMIT_INVALID", "Maximum simultaneous requests must be between 1 and 1000")
	case input.BudgetLimit != nil && !sharedChannelValueInRange(*input.BudgetLimit, 0.00000001, 1_000_000_000):
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_BUDGET_INVALID", "Consumption limit must be between 0.00000001 and 1000000000")
	case !sharedChannelValueInRange(*input.BudgetMultiplier, 0.000001, 1000):
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_MULTIPLIER_INVALID", "Budget multiplier must be between 0.000001 and 1000")
	case input.ExpiresAt != nil && input.BudgetLimit != nil:
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_END_MODE_CONFLICT", "Choose either time-based or budget-based access")
	case input.Published && input.ExpiresAt == nil && input.BudgetLimit == nil:
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_END_REQUIRED", "Set an expiry or consumption limit before publishing")
	case input.Published && input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()):
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_EXPIRY_INVALID", "The expiry must be in the future")
	}
	return s.repo.Save(ctx, id, input)
}

// 展示名称独立于分组的模型权限，仅做边界校验和有序去重。
func normalizeSharedChannelModels(input []string) ([]string, error) {
	invalid := infraerrors.BadRequest("SHARED_CHANNEL_MODELS_INVALID", "Use at most 100 model names, each 1 to 200 characters without control characters")
	if len(input) > 100 {
		return nil, invalid
	}
	models := make([]string, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, value := range input {
		model := strings.TrimSpace(value)
		if model == "" {
			continue
		}
		if utf8.RuneCountInString(model) > 200 || strings.ContainsFunc(model, unicode.IsControl) {
			return nil, invalid
		}
		if !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	return models, nil
}

func sharedChannelValueInRange(value, min, max float64) bool {
	return !math.IsNaN(value) && value >= min && value <= max
}

func (s *SharedChannelService) CreateKey(ctx context.Context, publicationID, userID int64, name string) (*APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, infraerrors.BadRequest("SHARED_CHANNEL_KEY_NAME_INVALID", "Key name must be 1 to 100 characters")
	}
	return s.keys.createSharedChannelKey(ctx, publicationID, userID, CreateAPIKeyRequest{Name: name})
}
