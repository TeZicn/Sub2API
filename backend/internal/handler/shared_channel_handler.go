package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SharedChannelHandler struct {
	service *service.SharedChannelService
}

func NewSharedChannelHandler(svc *service.SharedChannelService) *SharedChannelHandler {
	return &SharedChannelHandler{service: svc}
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
	response.Success(c, items)
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

func (h *SharedChannelHandler) AdminDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.BadRequest(c, "Invalid shared channel ID")
		return
	}
	if !response.ErrorFrom(c, h.service.Delete(c.Request.Context(), id)) {
		response.Success(c, gin.H{"deleted": true})
	}
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
