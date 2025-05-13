package auth

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"text/template"
	"time"

	_ "embed"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/auth"
	"github.com/openmined/syftbox/internal/server/email"
)

//go:embed emailOTP.html.tmpl
var emailTemplate string

type AuthHandler struct {
	auth          *auth.AuthService
	emailTemplate *template.Template
}

func New(auth *auth.AuthService) *AuthHandler {
	return &AuthHandler{
		auth:          auth,
		emailTemplate: template.Must(template.New("emailTemplate").Parse(emailTemplate)),
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

	emailOTP, err := h.auth.GenerateOTP(ctx, req.Email)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidEmail) {
			ctx.Error(fmt.Errorf("invalid email: %w", err))
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}
		ctx.Error(fmt.Errorf("failed to generate verification code: %w", err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	if err := h.sendEmailOTP(ctx, req.Email, emailOTP); err != nil {
		ctx.Error(fmt.Errorf("failed to send email: %w", err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
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

	if err := h.auth.VerifyOTP(ctx, req.Email, req.Code); err != nil {
		ctx.Error(fmt.Errorf("failed to verify OTP: %w", err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	accessToken, refreshToken, err := h.auth.GenerateTokens(ctx, req.Email)
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

func (h *AuthHandler) sendEmailOTP(ctx *gin.Context, to, code string) error {
	var buf bytes.Buffer
	if err := h.emailTemplate.Execute(&buf, map[string]any{
		"Email": to,
		"Code":  code,
		"Year":  time.Now().Year(),
	}); err != nil {
		return err
	}

	return email.Send(ctx.Request.Context(), &email.EmailData{
		FromName:  "SyftBox",
		FromEmail: "auth@syftbox.openmined.org",
		ToEmail:   to,
		Subject:   "SyftBox Verification Code",
		HTMLBody:  buf.String(),
	})
}
