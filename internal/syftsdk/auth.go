package syftsdk

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmined/syftbox/internal/utils"
	"resty.dev/v3"
)

const (
	authOtpRequest = "/auth/otp/request"
	authOtpVerify  = "/auth/otp/verify"
	authRefresh    = "/auth/refresh"
)

var (
	regexOTP = regexp.MustCompile(`^[0-9A-Z]{8}$`)
)

// VerifyEmail starts the Email verification flow by requesting a one-time password (OTP) from the server.
func VerifyEmail(ctx context.Context, serverURL string, email string) error {
	var sdkErr SyftSDKError

	if !utils.IsValidURL(serverURL) {
		return ErrNoServerURL
	}

	if !utils.IsValidEmail(email) {
		return ErrInvalidEmail
	}

	client := resty.New().SetBaseURL(serverURL)

	res, err := client.R().
		SetContext(ctx).
		SetBody(&VerifyEmailRequest{
			Email: email,
		}).
		SetError(&sdkErr).
		Post(authOtpRequest)

	if err != nil {
		return fmt.Errorf("sdk: request verification code: %w", err)
	}

	if res.IsError() {
		if sdkErr.Error != "" {
			return fmt.Errorf("sdk: request verification code: %q - %q", res.Status(), sdkErr.Error)
		}
		return fmt.Errorf("sdk: request verification code: %q - %q", res.Status(), res.String())
	}

	return nil
}

func VerifyEmailCode(ctx context.Context, serverURL string, codeReq *VerifyEmailCodeRequest) (*AuthTokenResponse, error) {
	var resp AuthTokenResponse
	var sdkErr SyftSDKError

	if !utils.IsValidURL(serverURL) {
		return nil, ErrNoServerURL
	}

	if !IsValidOTP(codeReq.Code) {
		return nil, ErrInvalidOTP
	}

	client := resty.New().SetBaseURL(serverURL)

	res, err := client.R().
		SetContext(ctx).
		SetBody(codeReq).
		SetResult(&resp).
		SetError(&sdkErr).
		Post(authOtpVerify)

	if err != nil {
		return nil, fmt.Errorf("sdk: verify email code: %w", err)
	}

	if res.IsError() {
		if sdkErr.Error != "" {
			return nil, fmt.Errorf("sdk: verify email code: %q - %q", res.Status(), sdkErr.Error)
		}
		return nil, fmt.Errorf("sdk: verify email code: %q - %q", res.Status(), res.String())
	}

	return &resp, nil
}

func RefreshAuthTokens(ctx context.Context, serverURL string, refreshToken string) (*AuthTokenResponse, error) {
	var resp AuthTokenResponse
	var sdkErr SyftSDKError

	if !utils.IsValidURL(serverURL) {
		return nil, ErrNoServerURL
	}

	if refreshToken == "" {
		return nil, ErrNoRefreshToken
	}

	client := resty.New().SetBaseURL(serverURL)

	res, err := client.R().
		SetContext(ctx).
		SetBody(&RefreshTokenRequest{
			RefreshToken: refreshToken,
		}).
		SetResult(&resp).
		SetError(&sdkErr).
		Post(authRefresh)

	if err != nil {
		return nil, fmt.Errorf("sdk: refresh auth tokens: %w", err)
	}

	if res.IsError() {
		if sdkErr.Error != "" {
			return nil, fmt.Errorf("sdk: refresh auth tokens: %q - %q", res.Status(), sdkErr.Error)
		}
		return nil, fmt.Errorf("sdk: refresh auth tokens: %q - %q", res.Status(), res.String())
	}

	return &resp, nil
}

func IsValidOTP(otp string) bool {
	return len(otp) == 8 && regexOTP.MatchString(otp)
}

func ParseToken(token string, tokenType AuthTokenType) (*AuthClaims, error) {
	if token == "" {
		return nil, fmt.Errorf("sdk: token is empty")
	}

	var claims AuthClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil {
		return nil, err
	}

	if claims.Type != tokenType {
		return nil, fmt.Errorf("sdk: invalid token type, expected %s, got %s", tokenType, claims.Type)
	}

	// check if expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("sdk: token expired, login again")
	}

	return &claims, nil
}
