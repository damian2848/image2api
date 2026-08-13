package handler

import (
	"net/http"

	"backend/internal/model"
	"backend/internal/service"
	"github.com/gin-gonic/gin"
)

type UserToolsHandler struct {
	keys *service.APIKeyService
	cdks *service.CDKService
}

func NewUserToolsHandler(keys *service.APIKeyService, cdks *service.CDKService) *UserToolsHandler {
	return &UserToolsHandler{
		keys: keys,
		cdks: cdks,
	}
}

func (h *UserToolsHandler) APIKeyGet(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	data, err := h.keys.Current(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to load api key"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *UserToolsHandler) APIKeyMint(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	data, err := h.keys.Mint(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to mint api key"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *UserToolsHandler) APIKeyDelete(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	if err := h.keys.Revoke(c.Request.Context(), user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to revoke api key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *UserToolsHandler) APIKeyDeleteOne(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	if err := h.keys.DeleteOne(c.Request.Context(), user.ID, c.Param("key_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "failed to delete api key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *UserToolsHandler) RedeemCDK(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未登录或会话已过期"})
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request body"})
		return
	}
	data, err := h.cdks.Redeem(c.Request.Context(), user.ID, body.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "amount": data["amount"], "credits": data["credits"]})
}

func currentUser(c *gin.Context) *model.User {
	value, ok := c.Get("current_user")
	if !ok {
		return nil
	}
	user, _ := value.(*model.User)
	return user
}
