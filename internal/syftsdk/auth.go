package syftsdk

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	authOtpRequest = "/auth/otp/request"
	authOtpVerify  = "/auth/otp/verify"
	authRefresh    = "/auth/refresh"
)

var (
	regexOTP   = regexp.MustCompile(`^[0-9A-Z]{4,8}$`)
	authClient = HTTPClient.Clone().
			SetCommonErrorResult(&APIError{})
)

// RequestEmailCode starts the Email verification flow by requesting a one-time password (OTP) from the server.
func RequestEmailCode(ctx context.Context, serverURL string, email string) error {
	if !utils.IsValidURL(serverURL) {
		return ErrNoServerURL
	}

	if !utils.IsValidEmail(email) {
		return ErrInvalidEmail
	}

	fullURL, err := url.JoinPath(serverURL, authOtpRequest)
	if err != nil {
		return fmt.Errorf("join path: %w", err)
	}

	res, err := authClient.R().
		SetContext(ctx).
		SetBody(&VerifyEmailRequest{
			Email: email,
		}).
		Post(fullURL)

	return handleAPIError(res, err, "request email code")
}

func VerifyEmailCode(ctx context.Context, serverURL string, codeReq *VerifyEmailCodeRequest) (apiResp *AuthTokenResponse, err error) {
	if !utils.IsValidURL(serverURL) {
		return nil, ErrNoServerURL
	}

	if !IsValidOTP(codeReq.Code) {
		return nil, ErrInvalidOTP
	}

	fullURL, err := url.JoinPath(serverURL, authOtpVerify)
	if err != nil {
		return nil, fmt.Errorf("join path: %w", err)
	}

	res, err := authClient.R().
		SetContext(ctx).
		SetBody(codeReq).
		SetSuccessResult(&apiResp).
		Post(fullURL)

	if err := handleAPIError(res, err, "verify email code"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

func RefreshAuthTokens(ctx context.Context, serverURL string, refreshToken string) (apiResp *AuthTokenResponse, err error) {
	if !utils.IsValidURL(serverURL) {
		return nil, ErrNoServerURL
	}

	if refreshToken == "" {
		return nil, ErrNoRefreshToken
	}

	fullURL, err := url.JoinPath(serverURL, authRefresh)
	if err != nil {
		return nil, fmt.Errorf("join path: %w", err)
	}

	res, err := authClient.R().
		SetContext(ctx).
		SetBody(&RefreshTokenRequest{
			RefreshToken: refreshToken,
		}).
		SetSuccessResult(&apiResp).
		Post(fullURL)

	if err := handleAPIError(res, err, "refresh auth tokens"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

func IsValidOTP(otp string) bool {
	return len(otp) == 8 && regexOTP.MatchString(otp)
}

func ParseToken(token string, tokenType AuthTokenType) (*AuthClaims, error) {
	if token == "" {
		return nil, fmt.Errorf("token is empty")
	}

	var claims AuthClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	if claims.Type != tokenType {
		return nil, fmt.Errorf("invalid token type, expected %s, got %s", tokenType, claims.Type)
	}

	// check if expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("sdk: token expired, login again")
	}

	return &claims, nil
}
