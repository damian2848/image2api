package handler

import (
	"errors"
	"net/http"

	"backend/internal/service"
	"github.com/gin-gonic/gin"
)

type SystemUpdateHandler struct {
	updater *service.SystemUpdateService
}

func NewSystemUpdateHandler(updater *service.SystemUpdateService) *SystemUpdateHandler {
	return &SystemUpdateHandler{updater: updater}
}

func (h *SystemUpdateHandler) Status(c *gin.Context) {
	status, err := h.updater.Status(c.Request.Context(), c.Query("refresh") == "true")
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *SystemUpdateHandler) Start(c *gin.Context) {
	status, err := h.updater.Start(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, status)
}

func (h *SystemUpdateHandler) writeError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrUpdaterDisabled) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "在线更新尚未在宿主机配置"})
		return
	}
	var updaterErr *service.UpdaterError
	if errors.As(err, &updaterErr) {
		c.JSON(updaterErr.Status, gin.H{"detail": updaterErr.Detail})
		return
	}
	c.JSON(http.StatusBadGateway, gin.H{"detail": "无法连接宿主机更新器: " + err.Error()})
}
