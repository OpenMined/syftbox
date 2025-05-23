package auth

import (
	"context"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/server/email"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func getTestAuthConfig() *Config {
	return &Config{
		Enabled:            true,
		TokenIssuer:        "https://issuer.com",
		RefreshTokenSecret: "refresh-secret",
		AccessTokenSecret:  "access-secret",
		RefreshTokenExpiry: time.Minute,
		AccessTokenExpiry:  time.Second * 10,
		EmailAddr:          "info@openmined.org",
		EmailOTPLength:     6,
		EmailOTPExpiry:     2 * time.Minute,
	}
}

type MockEmailService struct {
	mock.Mock
}

func (m *MockEmailService) IsEnabled() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockEmailService) Send(ctx context.Context, data *email.EmailInfo) error {
	args := m.Called(ctx, data)
	return args.Error(0)
}

func NewMockEmailService() *MockEmailService {
	emailSvc := &MockEmailService{}
	emailSvc.On("IsEnabled").Return(true)
	emailSvc.On("Send", mock.Anything, mock.Anything).Return(nil)
	return emailSvc
}

func NewMockEmailServiceDisabled() *MockEmailService {
	emailSvc := &MockEmailService{}
	emailSvc.On("IsEnabled").Return(false)
	return emailSvc
}

var _ email.Service = (*MockEmailService)(nil)

func TestAuthService_IsEnabled(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())
	assert.True(t, svc.IsEnabled())

	cfg.Enabled = false
	svc = NewAuthService(cfg, NewMockEmailServiceDisabled())
	assert.False(t, svc.IsEnabled())
}

func TestAuthService_OTP(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	otp, err := svc.generateOTP("user@email.com")
	assert.NoError(t, err)
	assert.Len(t, otp, cfg.EmailOTPLength)

	// Should verify successfully
	err = svc.verifyOTP("user@email.com", otp)
	assert.NoError(t, err)

	// Should fail if OTP is reused
	err = svc.verifyOTP("user@email.com", otp)
	assert.Error(t, err)

	// Should fail for wrong OTP
	otp2, _ := svc.generateOTP("user@email.com")
	err = svc.verifyOTP("user@email.com", "wrong1")
	assert.Error(t, err)

	// Should fail for wrong email
	err = svc.verifyOTP("not-an-email", otp2)
	assert.Error(t, err)
}

func TestAuthService_GenerateTokensPair(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	user := "user@email.com"
	otp, err := svc.generateOTP(user)
	require.NoError(t, err)

	access, refresh, err := svc.GenerateTokensPair(context.Background(), user, otp)
	assert.NoError(t, err)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)

	claims, err := svc.ValidateAccessToken(context.Background(), access)
	assert.NoError(t, err)
	assert.Equal(t, user, claims.Subject)
	assert.Equal(t, AccessToken, claims.Type)

	rclaims, err := svc.ValidateRefreshToken(context.Background(), refresh)
	assert.NoError(t, err)
	assert.Equal(t, user, rclaims.Subject)
	assert.Equal(t, RefreshToken, rclaims.Type)
}

func TestAuthService_RefreshToken(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	user := "user@email.com"
	otp, err := svc.generateOTP(user)
	require.NoError(t, err)
	_, refresh, err := svc.GenerateTokensPair(context.Background(), user, otp)
	require.NoError(t, err)

	access2, refresh2, err := svc.RefreshToken(context.Background(), refresh)
	assert.NoError(t, err)
	assert.NotEmpty(t, access2)
	assert.NotEmpty(t, refresh2)

	// Invalid refresh token
	_, _, err = svc.RefreshToken(context.Background(), "invalid.token")
	assert.Error(t, err)
}

func TestAuthService_ValidateAccessToken_Errors(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	_, err := svc.ValidateAccessToken(context.Background(), "")
	assert.Error(t, err)

	// Token of wrong type
	refresh, _ := newRefreshToken("user@email.com", cfg.TokenIssuer, cfg.RefreshTokenSecret, time.Minute)
	_, err = svc.ValidateAccessToken(context.Background(), refresh)
	assert.Error(t, err)
}

func TestAuthService_ValidateRefreshToken_Errors(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	_, err := svc.ValidateRefreshToken(context.Background(), "")
	assert.Error(t, err)

	// Token of wrong type
	access, _ := newAccessToken("user@email.com", cfg.TokenIssuer, cfg.AccessTokenSecret, time.Minute)
	_, err = svc.ValidateRefreshToken(context.Background(), access)
	assert.Error(t, err)
}

func TestAuthService_generateOTPEmail(t *testing.T) {
	cfg := getTestAuthConfig()
	svc := NewAuthService(cfg, NewMockEmailService())

	email := "user@email.com"
	code := "ABC123"
	html, err := svc.generateOTPEmail(email, code)
	assert.NoError(t, err)
	assert.Contains(t, html, email)
	assert.Contains(t, html, code)
	assert.Contains(t, html, "SyftBox")      // Should contain branding
	assert.Contains(t, html, "Verification") // Should contain subject/heading
	assert.Contains(t, html, "2 minutes")    // Should mention validity
}

func TestAuthService_SendOTP(t *testing.T) {
	cfg := getTestAuthConfig()
	emailSvc := NewMockEmailService()
	svc := NewAuthService(cfg, emailSvc)

	email := "user@email.com"

	err := svc.SendOTP(context.Background(), email)
	assert.NoError(t, err)
	emailSvc.AssertExpectations(t)
}

func TestAuthService_SendOTP_EmailDisabled(t *testing.T) {
	cfg := getTestAuthConfig()
	emailSvc := NewMockEmailServiceDisabled()
	svc := NewAuthService(cfg, emailSvc)

	email := "user@email.com"

	err := svc.SendOTP(context.Background(), email)
	assert.NoError(t, err)
	emailSvc.AssertExpectations(t)
}
