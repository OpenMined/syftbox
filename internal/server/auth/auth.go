package auth

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/openmined/syftbox/internal/server/email"
	"github.com/openmined/syftbox/internal/utils"
)

type AuthService struct {
	config        *Config
	codes         *expirable.LRU[string, string]
	emailTemplate *template.Template
}

func NewAuthService(config *Config) *AuthService {
	return &AuthService{
		config:        config,
		codes:         expirable.NewLRU[string, string](0, nil, config.EmailOTPExpiry), // 0 = LRU off
		emailTemplate: template.Must(template.New("emailTemplate").Parse(emailTemplate)),
	}
}

func (s *AuthService) IsEnabled() bool {
	return s.config.Enabled
}

func (s *AuthService) SendOTP(ctx context.Context, userEmail string) error {
	// If auth is disabled, we don't need to send an OTP
	if !s.IsEnabled() {
		return nil
	}

	// Generate an OTP
	otp, err := s.generateOTP(userEmail)
	if err != nil {
		return err
	}

	// Send the OTP to the user's email
	return s.sendOTPEmail(ctx, userEmail, otp)
}

func (s *AuthService) GenerateTokens(ctx context.Context, userEmail string, otp string) (string, string, error) {
	// If auth is disabled, we don't need to generate tokens
	if !s.IsEnabled() {
		slog.Debug("auth is disabled, will not generate tokens")
		return "", "", nil
	}

	// Verify the OTP
	if err := s.verifyOTP(userEmail, otp); err != nil {
		return "", "", fmt.Errorf("failed to generate token pair: %w", err)
	}

	// Generate tokens
	// maybe persist the refresh token id in a db for revocation
	accessToken, refreshToken, err := generateTokenPair(userEmail, s.config)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate token pair: %w", err)
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, oldRefreshToken string) (string, string, error) {
	if oldRefreshToken == "" {
		return "", "", ErrInvalidRequestToken
	}

	// verify the old refresh token
	claims, err := s.ValidateRefreshToken(ctx, oldRefreshToken)
	if err != nil {
		return "", "", fmt.Errorf("failed to refresh token pair: %w", err)
	}

	// generate a new token pair
	// maybe persist the refresh token id in a db for revocation?
	accessToken, refreshToken, err := generateTokenPair(claims.Subject, s.config)
	if err != nil {
		return "", "", fmt.Errorf("failed to refresh token pair: %w", err)
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) ValidateAccessToken(ctx context.Context, accessToken string) (*Claims, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("invalid access token")
	}

	// parse the claims
	claims, err := ParseClaims(accessToken, s.config.AccessTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid access token: %w", err)
	}

	// verify token type
	if claims.Type != AccessToken {
		return nil, fmt.Errorf("invalid access token: wrong token type got %q", claims.Type)
	}

	// maybe add more checks here if needed (e.g., check against a denylist)

	return claims, nil
}

func (s *AuthService) ValidateRefreshToken(ctx context.Context, refreshToken string) (*Claims, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("invalid refresh token")
	}

	claims, err := ParseClaims(refreshToken, s.config.RefreshTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	if claims.Type != RefreshToken {
		return nil, fmt.Errorf("invalid refresh token: wrong token type got %q", claims.Type)
	}

	return claims, nil
}

func (s *AuthService) generateOTP(userEmail string) (string, error) {
	if !utils.IsValidEmail(userEmail) {
		return "", ErrInvalidEmail
	}

	otp, err := utils.RandBase34(s.config.EmailOTPLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate OTP: %w", err)
	}

	s.codes.Add(userEmail, otp)

	return otp, nil
}

func (s *AuthService) verifyOTP(userEmail string, otp string) error {
	if err := utils.ValidateEmail(userEmail); err != nil {
		return err
	}

	if len(otp) != s.config.EmailOTPLength {
		return ErrInvalidOTP
	}

	storedOTP, ok := s.codes.Get(userEmail)
	if !ok || storedOTP != otp {
		return ErrInvalidOTP
	}

	s.codes.Remove(userEmail)
	return nil
}

func (s *AuthService) sendOTPEmail(ctx context.Context, to, code string) error {
	htmlBody, err := s.generateOTPEmail(to, code)
	if err != nil {
		return fmt.Errorf("failed to generate email: %w", err)
	}

	return email.Send(ctx, &email.EmailInfo{
		FromName:  "SyftBox",
		FromEmail: s.config.EmailAddr,
		Subject:   "SyftBox Verification Code",
		ToEmail:   to,
		HTMLBody:  htmlBody,
	})
}

func (s *AuthService) generateOTPEmail(to, code string) (string, error) {
	var buf bytes.Buffer

	if err := s.emailTemplate.Execute(&buf, map[string]any{
		"Email":        to,
		"Code":         code,
		"Year":         time.Now().Year(),
		"ValidityMins": s.config.EmailOTPExpiry.Minutes(),
	}); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func generateTokenPair(subject string, config *Config) (accessToken string, refreshToken string, err error) {
	// generate access token
	// maybe save the access token ID for revocation?
	accessToken, err = newAccessToken(subject, config.TokenIssuer, config.AccessTokenSecret, config.AccessTokenExpiry)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate access token: %w", err)
	}

	// generate refresh token
	refreshToken, err = newRefreshToken(subject, config.TokenIssuer, config.RefreshTokenSecret, config.RefreshTokenExpiry)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return accessToken, refreshToken, nil
}

func newAccessToken(subject, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return newToken(subject, issuer, jwtSecret, expiry, AccessToken)
}

func newRefreshToken(subject, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return newToken(subject, issuer, jwtSecret, expiry, RefreshToken)
}

func newToken(subject, issuer, jwtSecret string, expiry time.Duration, tokenType AuthTokenType) (string, error) {
	var expiryTime *jwt.NumericDate

	if expiry > 0 {
		expiryTime = jwt.NewNumericDate(time.Now().Add(expiry))
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   subject,
			Issuer:    issuer,
			ExpiresAt: expiryTime,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Type: tokenType,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}
