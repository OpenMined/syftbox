package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"text/template"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/openmined/syftbox/internal/server/email"
	"github.com/openmined/syftbox/internal/utils"
)

type EmailString = string

type OTPString = string

type AuthService struct {
	config        *Config
	codes         *expirable.LRU[EmailString, OTPString]
	emailTemplate *template.Template
	emailSvc      email.Service
}

func NewAuthService(config *Config, emailSvc email.Service) *AuthService {
	return &AuthService{
		config:        config,
		codes:         expirable.NewLRU[EmailString, OTPString](0, nil, config.EmailOTPExpiry), // 0 = LRU off
		emailTemplate: template.Must(template.New("emailTemplate").Parse(emailTemplate)),
		emailSvc:      emailSvc,
	}
}

func (s *AuthService) IsEnabled() bool {
	return s.config.Enabled
}

func (s *AuthService) SendOTP(ctx context.Context, userEmail EmailString) error {
	// Generate an OTP
	otp, err := s.generateOTP(userEmail)
	if err != nil {
		return err
	}

	// If auth is disabled, we don't need to send an OTP
	if !s.emailSvc.IsEnabled() {
		slog.Warn("email is disabled", "email", userEmail, "otp", otp)
		return nil
	}

	// Send the OTP to the user's email
	return s.sendOTPEmail(ctx, userEmail, otp)
}

func (s *AuthService) GenerateTokensPair(ctx context.Context, userEmail EmailString, otp OTPString) (string, string, error) {
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

func (s *AuthService) generateOTP(userEmail EmailString) (OTPString, error) {
	if !utils.IsValidEmail(userEmail) {
		return "", ErrInvalidEmail
	}

	otp, err := randOTP(s.config.EmailOTPLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate OTP: %w", err)
	}

	s.codes.Add(userEmail, otp)

	return otp, nil
}

func (s *AuthService) verifyOTP(userEmail EmailString, otp OTPString) error {
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

func (s *AuthService) sendOTPEmail(ctx context.Context, to EmailString, code OTPString) error {
	htmlBody, err := s.generateOTPEmail(to, code)
	if err != nil {
		return fmt.Errorf("failed to generate email: %w", err)
	}

	return s.emailSvc.Send(ctx, &email.EmailInfo{
		FromName:  "SyftBox",
		FromEmail: s.config.EmailAddr,
		Subject:   "SyftBox Verification Code",
		ToEmail:   to,
		HTMLBody:  htmlBody,
	})
}

func (s *AuthService) generateOTPEmail(to EmailString, code OTPString) (string, error) {
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

func generateTokenPair(subject EmailString, config *Config) (accessToken string, refreshToken string, err error) {
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

func newAccessToken(subject EmailString, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return newToken(subject, issuer, jwtSecret, expiry, AccessToken)
}

func newRefreshToken(subject EmailString, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return newToken(subject, issuer, jwtSecret, expiry, RefreshToken)
}

func newToken(subject EmailString, issuer, jwtSecret string, expiry time.Duration, tokenType AuthTokenType) (string, error) {
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

func randOTP(length int) (OTPString, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid OTP length: %d", length)
	}

	otpChars := make([]byte, length)
	for i := range otpChars {
		num, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", fmt.Errorf("failed to generate random digit: %w", err)
		}
		otpChars[i] = byte(num.Int64() + '0') // convert number to ascii
	}

	return string(otpChars), nil
}
