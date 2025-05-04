package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

var (
	ErrInvalidEmail = errors.New("invalid email address")
	ErrInvalidOTP   = errors.New("invalid OTP")
)

type AuthService struct {
	config *Config
	codes  *expirable.LRU[string, string]
}

func NewAuthService(config *Config) *AuthService {
	return &AuthService{
		config: config,
		codes:  expirable.NewLRU[string, string](0, nil, config.EmailOTPExpiry), // 0 = LRU off
	}
}

func (s *AuthService) IsEnabled() bool {
	return s.config.Enabled
}

func (s *AuthService) GenerateOTP(ctx context.Context, userEmail string) (string, error) {
	if !validEmail(userEmail) {
		return "", ErrInvalidEmail
	}

	otp, err := generateOTP(s.config.EmailOTPLength)
	if err != nil {
		return "", err
	}

	s.codes.Add(userEmail, otp)

	return otp, nil
}

func (s *AuthService) VerifyOTP(ctx context.Context, userEmail string, otp string) error {
	if !validEmail(userEmail) {
		return ErrInvalidEmail
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

func (s *AuthService) GenerateTokens(ctx context.Context, userEmail string) (string, string, error) {
	if !validEmail(userEmail) {
		return "", "", ErrInvalidEmail
	}

	accessToken, refreshToken, err := GenerateTokens(userEmail, s.config)
	if err != nil {
		return "", "", err
	}

	// todo persist the refresh token id in a db for revocation

	return accessToken, refreshToken, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, oldRefreshToken string) (string, string, error) {
	if oldRefreshToken == "" {
		return "", "", errors.New("refresh token is required")
	}

	claims, err := s.ValidateRefreshToken(ctx, oldRefreshToken)
	if err != nil {
		return "", "", err
	}

	// todo persist the refresh token id in a db for revocation

	accessToken, refreshToken, err := GenerateTokens(claims.Subject, s.config)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) ValidateAccessToken(ctx context.Context, accessToken string) (*Claims, error) {
	if accessToken == "" {
		return nil, errors.New("access token is required")
	}

	claims, err := ParseClaims(accessToken, s.config.AccessTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid access token: %w", err)
	}

	if claims.Type != AccessToken {
		return nil, errors.New("invalid token type: expected access token")
	}

	// Potentially add more checks here if needed (e.g., check against a denylist)

	return claims, nil
}

func (s *AuthService) ValidateRefreshToken(ctx context.Context, refreshToken string) (*Claims, error) {
	if refreshToken == "" {
		return nil, errors.New("refresh token is required")
	}

	claims, err := ParseClaims(refreshToken, s.config.RefreshTokenSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	if claims.Type != RefreshToken {
		return nil, errors.New("invalid token type: expected refresh token")
	}

	return claims, nil
}
