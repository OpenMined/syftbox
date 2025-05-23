package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth"
)

type AuthHandler struct {
	auth *auth.AuthService
}

func New(auth *auth.AuthService) *AuthHandler {
	return &AuthHandler{
		auth: auth,
	}
}

func (h *AuthHandler) OTPRequest(ctx *gin.Context) {
	var req OTPRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.Error(fmt.Errorf("failed to bind json: %w", err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	if err := h.auth.SendOTP(ctx, req.Email); err != nil {
		ctx.Error(fmt.Errorf("failed to send OTP: %w", err))
		if errors.Is(err, auth.ErrInvalidEmail) {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
		} else {
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
		}
		return
	}

	ctx.String(http.StatusOK, "")
}

func (h *AuthHandler) OTPVerify(ctx *gin.Context) {
	var req OTPVerifyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.Error(fmt.Errorf("failed to bind json: %w", err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	accessToken, refreshToken, err := h.auth.GenerateTokensPair(ctx, req.Email, req.Code)
	if err != nil {
		ctx.Error(fmt.Errorf("failed to generate tokens: %w", err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, &OTPVerifyResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (h *AuthHandler) Refresh(ctx *gin.Context) {
	var req RefreshRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.Error(fmt.Errorf("failed to bind json: %w", err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	accessToken, refreshToken, err := h.auth.RefreshToken(ctx, req.OldRefreshToken)
	if err != nil {
		ctx.Error(fmt.Errorf("failed to refresh token: %w", err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, &RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}
