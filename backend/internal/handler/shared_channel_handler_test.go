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

type sharedChannelHandlerRepo struct {
	service.SharedChannelRepository
	published []service.PublishedSharedChannel
	deletedID int64
}

func (r *sharedChannelHandlerRepo) ListPublished(context.Context, int64) ([]service.PublishedSharedChannel, error) {
	return r.published, nil
}

func (r *sharedChannelHandlerRepo) Delete(_ context.Context, id int64) error {
	r.deletedID = id
	return nil
}

func TestSharedChannelListUsesConfiguredDisplayModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &sharedChannelHandlerRepo{published: []service.PublishedSharedChannel{{ID: 1, GroupID: 12, Models: []string{"Custom model", "second-model"}}}}
	h := NewSharedChannelHandler(service.NewSharedChannelService(repo, nil))
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/shared-channels", nil)
	ctx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1})
	h.List(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	var payload struct {
		Data []service.PublishedSharedChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []string{"Custom model", "second-model"}, payload.Data[0].Models)
	require.NotContains(t, rec.Body.String(), "group_id")
}

func TestSharedChannelDeleteValidatesID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &sharedChannelHandlerRepo{}
	h := NewSharedChannelHandler(service.NewSharedChannelService(repo, nil))
	router := gin.New()
	router.DELETE("/shared-channels/:id", h.AdminDelete)
	for _, id := range []string{"invalid", "0", "-1"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/shared-channels/"+id, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Zero(t, repo.deletedID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/shared-channels/9", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(9), repo.deletedID)
}
