package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSharedChannelModelsMatchGatewayCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			group := &service.Group{ID: 9, Platform: platform, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"public-model"}}}
			accountPlatform := platform
			if platform == service.PlatformComposite {
				accountPlatform = service.PlatformOpenAI
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				9: {{ID: 1, Platform: accountPlatform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{
					"model_mapping": map[string]any{"public-model": "private-upstream", "hidden-model": "hidden-upstream"},
				}}},
			}})
			models, err := h.sharedChannelModelIDs(context.Background(), group)
			require.NoError(t, err)
			require.Equal(t, []string{"public-model"}, models)
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
			h.Models(ctx)
			require.Equal(t, http.StatusOK, rec.Code)
			var response gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, modelIDsForTest(response.Data), models)
		})
	}
}

func TestPublishedSharedChannelDoesNotExposeGroupID(t *testing.T) {
	body, err := json.Marshal(service.PublishedSharedChannel{GroupID: 12, Models: []string{"public-model"}})
	require.NoError(t, err)
	require.NotContains(t, string(body), "group_id")
	require.Contains(t, string(body), `"models":["public-model"]`)
}

func TestSharedChannelDefaultModelsMatchGatewayCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformTypeSafe, service.PlatformAntigravity, service.PlatformOpenCodeGo, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			group := &service.Group{ID: 9, Platform: platform}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{})
			models, err := h.sharedChannelModelIDs(context.Background(), group)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			ctx.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: group})
			h.Models(ctx)
			require.Equal(t, http.StatusOK, rec.Code)
			var response gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, modelIDsForTest(response.Data), models)
		})
	}
}
