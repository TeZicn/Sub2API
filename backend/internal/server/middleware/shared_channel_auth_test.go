//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sharedAuthRepository struct {
	service.SharedChannelRepository
	member *service.SharedChannelMembership
}

func (r *sharedAuthRepository) FindMembership(context.Context, int64) (*service.SharedChannelMembership, error) {
	return r.member, nil
}

func TestSharedChannelAuthWithoutBalanceStillRequiresLiveGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		cfg := &config.Config{RunMode: config.RunModeStandard}
		member := &service.SharedChannelMembership{ID: 1, UserID: 7, GroupID: 42, MaxConcurrency: 8, Members: 1, Rank: 1, PersonalConcurrency: 5}
		shared := &sharedAuthRepository{member: member}
		repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
			return &service.APIKey{ID: 1, UserID: 7, Key: "test-key", GroupID: &member.GroupID, SharedChannelMembershipID: &member.ID, Status: service.StatusActive,
				User:  &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 0},
				Group: &service.Group{ID: 42, Platform: service.PlatformOpenAI, Status: service.StatusActive, IsExclusive: true, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}}, nil
		}}
		svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
		svc.SetSharedChannelRepository(shared)
		r := gin.New()
		if google {
			r.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
		} else {
			r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		}
		r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })
		request := func() int {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.Header.Set("Authorization", "Bearer test-key")
			r.ServeHTTP(w, req)
			return w.Code
		}
		require.Equal(t, http.StatusOK, request())
		shared.member = nil
		require.Equal(t, http.StatusUnauthorized, request())
	}
}
