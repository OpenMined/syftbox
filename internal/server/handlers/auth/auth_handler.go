package auth

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth"
	"github.com/openmined/syftbox/internal/server/handlers/api"
	"github.com/openmined/syftbox/internal/utils"
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
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	if !utils.IsValidEmail(req.Email) {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("invalid email"))
		return
	}

	if err := h.auth.SendOTP(ctx, req.Email); err != nil {
		api.AbortWithError(ctx, http.StatusInternalServerError, api.CodeAuthNotificationFailed, fmt.Errorf("failed to send OTP: %w", err))
		return
	}

	ctx.String(http.StatusOK, "")
}

func (h *AuthHandler) OTPVerify(ctx *gin.Context) {
	var req OTPVerifyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	accessToken, refreshToken, err := h.auth.GenerateTokensPair(ctx, req.Email, req.Code)
	if err != nil {
		api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthTokenGenerationFailed, err)
		return
	}

	ctx.PureJSON(http.StatusOK, &OTPVerifyResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

func (h *AuthHandler) Refresh(ctx *gin.Context) {
	var req RefreshRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		api.AbortWithError(ctx, http.StatusBadRequest, api.CodeInvalidRequest, fmt.Errorf("failed to bind json: %w", err))
		return
	}

	accessToken, refreshToken, err := h.auth.RefreshToken(ctx, req.OldRefreshToken)
	if err != nil {
		api.AbortWithError(ctx, http.StatusUnauthorized, api.CodeAuthTokenRefreshFailed, err)
		return
	}

	ctx.PureJSON(http.StatusOK, &RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}
