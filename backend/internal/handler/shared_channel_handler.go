package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SharedChannelHandler struct {
	service *service.SharedChannelService
	groups  service.GroupRepository
	gateway *GatewayHandler
}

func NewSharedChannelHandler(svc *service.SharedChannelService, groups service.GroupRepository, gateway *GatewayHandler) *SharedChannelHandler {
	return &SharedChannelHandler{service: svc, groups: groups, gateway: gateway}
}

func (h *SharedChannelHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.service.ListPublished(c.Request.Context(), subject.UserID)
	if response.ErrorFrom(c, err) {
		return
	}
	for i := range items {
		group, err := h.groups.GetByID(c.Request.Context(), items[i].GroupID)
		if response.ErrorFrom(c, err) {
			return
		}
		models, err := h.gateway.sharedChannelModelIDs(c.Request.Context(), group)
		if response.ErrorFrom(c, err) {
			return
		}
		items[i].Models = append([]string{}, models...)
	}
	response.Success(c, items)
}

// 共享卡片与模型接口共用目录及白名单规则，不公开内部路由或上游模型映射。
func (h *GatewayHandler) sharedChannelModelIDs(ctx context.Context, group *service.Group) ([]string, error) {
	if group.Platform == service.PlatformOpenAI && group.CodexModelsManifestConfig.Enabled {
		if h.openAIGatewayService == nil {
			return nil, fmt.Errorf("OpenAI model discovery is not configured")
		}
		catalog, _, err := h.openAIGatewayService.FetchPinnedOpenAIModelsList(ctx, group, h.maxAccountSwitches, "")
		if err != nil {
			return nil, err
		}
		var payload struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(catalog.Body, &payload); err != nil {
			return nil, fmt.Errorf("decode shared channel model catalog: %w", err)
		}
		models := make([]string, 0, len(payload.Data))
		for _, model := range payload.Data {
			models = append(models, model.ID)
		}
		return models, nil
	}
	models, configured := h.configuredModelIDs(ctx, group, &group.ID, group.Platform)
	if configured {
		return models, nil
	}
	return defaultModelIDsForPlatform(gatewayModelsFallbackPlatform(group.Platform)), nil
}

func (h *SharedChannelHandler) AdminList(c *gin.Context) {
	items, err := h.service.List(c.Request.Context())
	if !response.ErrorFrom(c, err) {
		response.Success(c, items)
	}
}

func (h *SharedChannelHandler) AdminCreate(c *gin.Context) {
	h.save(c, 0)
}

func (h *SharedChannelHandler) AdminUpdate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.BadRequest(c, "Invalid shared channel ID")
		return
	}
	h.save(c, id)
}

func (h *SharedChannelHandler) save(c *gin.Context, id int64) {
	var input service.SharedChannelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid shared channel configuration")
		return
	}
	item, err := h.service.Save(c.Request.Context(), id, input)
	if response.ErrorFrom(c, err) {
		return
	}
	if id == 0 {
		response.Created(c, item)
		return
	}
	response.Success(c, item)
}

func (h *SharedChannelHandler) CreateKey(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.BadRequest(c, "Invalid shared channel ID")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid key configuration")
		return
	}
	key, err := h.service.CreateKey(c.Request.Context(), id, subject.UserID, input.Name)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, dto.APIKeyFromService(key).Localized(c.GetHeader("Accept-Language")))
}
