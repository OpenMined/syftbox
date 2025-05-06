package syftsdk

import (
	"context"
	"fmt"

	"resty.dev/v3"
)

const (
	authOtpRequest = "/auth/otp/request"
	authOtpVerify  = "/auth/otp/verify"
	authRefresh    = "/auth/refresh"
)

// VerifyEmail starts the Email verification flow by requesting a one-time password (OTP) from the server.
func VerifyEmail(ctx context.Context, serverURL string, email string) error {
	var sdkErr SyftSDKError

	client := resty.New().SetBaseURL(serverURL)

	res, err := client.R().
		SetContext(ctx).
		SetBody(&VerifyEmailRequest{
			Email: email,
		}).
		SetError(&sdkErr).
		Post(authOtpRequest)

	if err != nil {
		return err
	}

	if res.IsError() {
		if sdkErr.Error != "" {
			return fmt.Errorf("failed to request verification code: %s", sdkErr.Error)
		}
		return fmt.Errorf("failed to request verification code: %s", res.String())
	}

	return nil
}

func VerifyEmailCode(ctx context.Context, serverURL string, codeReq *VerifyEmailCodeRequest) (*AuthTokenResponse, error) {
	var resp AuthTokenResponse
	var sdkErr SyftSDKError

	client := resty.New().SetBaseURL(serverURL)

	res, err := client.R().
		SetContext(ctx).
		SetBody(codeReq).
		SetResult(&resp).
		SetError(&sdkErr).
		Post(authOtpVerify)

	if err != nil {
		return nil, err
	}

	if res.IsError() {
		if sdkErr.Error != "" {
			return nil, fmt.Errorf("failed to verify OTP: %s", sdkErr.Error)
		}
		return nil, fmt.Errorf("failed to verify OTP: %s", res.String())
	}

	return &resp, nil
}
