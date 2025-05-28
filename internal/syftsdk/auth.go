package syftsdk

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/version"
)

const (
	authOtpRequest = "/auth/otp/request"
	authOtpVerify  = "/auth/otp/verify"
	authRefresh    = "/auth/refresh"
)

var (
	regexOTP    = regexp.MustCompile(`^[0-9A-Z]{8}$`)
	guestClient = req.C().
			SetCommonRetryCount(3).
			SetCommonRetryFixedInterval(1*time.Second).
			SetUserAgent("SyftBox/"+version.Version).
			SetCommonHeader(HeaderSyftVersion, version.Version).
			SetCommonHeader(HeaderSyftDeviceId, utils.HWID).
			SetJsonMarshal(jsonMarshal).
			SetJsonUnmarshal(jsonUmarshal)
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

	fullURL, err := url.JoinPath(serverURL, authOtpRequest)
	if err != nil {
		return fmt.Errorf("join path: %w", err)
	}

	res, err := guestClient.R().
		SetContext(ctx).
		SetBody(&VerifyEmailRequest{
			Email: email,
		}).
		SetErrorResult(&sdkErr).
		Post(fullURL)

	if err != nil {
		return fmt.Errorf("request verification code: %w", err)
	}

	if res.IsErrorState() {
		return &sdkErr
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

	fullURL, err := url.JoinPath(serverURL, authOtpVerify)
	if err != nil {
		return nil, fmt.Errorf("join path: %w", err)
	}

	res, err := guestClient.R().
		SetContext(ctx).
		SetBody(codeReq).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkErr).
		Post(fullURL)

	if err != nil {
		return nil, fmt.Errorf("sdk: verify email code: %w", err)
	}

	if res.IsErrorState() {
		return nil, &sdkErr
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

	fullURL, err := url.JoinPath(serverURL, authRefresh)
	if err != nil {
		return nil, fmt.Errorf("join path: %w", err)
	}

	res, err := guestClient.R().
		SetContext(ctx).
		SetBody(&RefreshTokenRequest{
			RefreshToken: refreshToken,
		}).
		SetSuccessResult(&resp).
		SetErrorResult(&sdkErr).
		Post(fullURL)

	if err != nil {
		return nil, fmt.Errorf("sdk: refresh auth tokens: %w", err)
	}

	if res.IsErrorState() {
		return nil, &sdkErr
	}

	return &resp, nil
}

func IsValidOTP(otp string) bool {
	return len(otp) == 8 && regexOTP.MatchString(otp)
}

func parseToken(token string, tokenType AuthTokenType) (*AuthClaims, error) {
	if token == "" {
		return nil, fmt.Errorf("token is empty")
	}

	var claims AuthClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil {
		return nil, err
	}
	if claims.Type != tokenType {
		return nil, fmt.Errorf("invalid token type. expected %s, got %s", tokenType, claims.Type)
	}
	return &claims, nil
}
