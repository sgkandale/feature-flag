package flag

import (
	"errors"
	"net/http"

	"feature-flag/internal/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase *Usecase
}

func NewHandler(u *Usecase) *Handler {
	return &Handler{usecase: u}
}

// RegisterRoutes sets up all routes on the given router engine or group
func (h *Handler) RegisterRoutes(router *gin.Engine, authMiddleware gin.HandlerFunc, rateLimitMiddleware gin.HandlerFunc) {
	// Protected endpoints for CRUD operations
	api := router.Group("")
	api.Use(authMiddleware)
	{
		api.POST("/flags", h.CreateFlag)
		api.GET("/flags", h.ListFlags)
		api.GET("/flags/:key", h.GetFlag)
		api.PUT("/flags/:key", h.UpdateFlag)
		api.DELETE("/flags/:key", h.DeleteFlag)
	}

	// Evaluation endpoint - high traffic, protected by rate limiting & auth (if needed).
	// The prompt states: "the /evaluate endpoint especially needs to survive high call volume gracefully."
	// We apply rate limiting here as well.
	router.GET("/evaluate/:key", authMiddleware, rateLimitMiddleware, h.EvaluateFlag)
}

func (h *Handler) CreateFlag(c *gin.Context) {
	var req CreateFlagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flagModel := req.ToDomain()
	err := h.usecase.CreateFlag(c.Request.Context(), flagModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, NewFlagResponse(flagModel))
}

func (h *Handler) ListFlags(c *gin.Context) {
	env := c.Query("env") // Optional filter

	flags, err := h.usecase.ListFlags(c.Request.Context(), env)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, NewFlagListResponse(flags))
}

func (h *Handler) GetFlag(c *gin.Context) {
	key := c.Param("key")
	env := c.Query("env")

	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'env' is required"})
		return
	}

	flagModel, err := h.usecase.GetFlag(c.Request.Context(), key, env)
	if err != nil {
		if errors.Is(err, domain.ErrFlagNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Flag not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, NewFlagResponse(flagModel))
}

func (h *Handler) UpdateFlag(c *gin.Context) {
	key := c.Param("key")

	var req UpdateFlagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flagModel := req.ToDomain(key)
	err := h.usecase.UpdateFlag(c.Request.Context(), flagModel)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorizedOperation) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fetch updated model to return accurate timestamps
	fullModel, err := h.usecase.GetFlag(c.Request.Context(), key, flagModel.Environment)
	if err != nil {
		c.JSON(http.StatusOK, NewFlagResponse(flagModel))
		return
	}

	c.JSON(http.StatusOK, NewFlagResponse(fullModel))
}

func (h *Handler) DeleteFlag(c *gin.Context) {
	key := c.Param("key")
	env := c.Query("env")

	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'env' is required"})
		return
	}

	err := h.usecase.DeleteFlag(c.Request.Context(), key, env)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorizedOperation) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) EvaluateFlag(c *gin.Context) {
	key := c.Param("key")
	env := c.Query("env")
	callerID := c.Query("caller_id")

	if env == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'env' is required"})
		return
	}

	// Fallback to client IP if caller_id is not provided
	if callerID == "" {
		callerID = c.ClientIP()
	}

	enabled, err := h.usecase.EvaluateFlag(c.Request.Context(), key, env, callerID)
	if err != nil {
		// Log internal error but return false to client with a clean message
		_ = c.Error(err)
		
		// If flag not found, return 404
		if errors.Is(err, domain.ErrFlagNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Flag not found"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"key":         key,
			"environment": env,
			"enabled":     false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"key":         key,
		"environment": env,
		"enabled":     enabled,
	})
}
