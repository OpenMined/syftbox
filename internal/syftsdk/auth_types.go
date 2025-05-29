package syftsdk

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type AuthTokenType string

const (
	AccessToken  AuthTokenType = "access"
	RefreshToken AuthTokenType = "refresh"
)

type VerifyEmailRequest struct {
	Email string `json:"email"`
}

type VerifyEmailCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type AuthTokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type AuthClaims struct {
	Type AuthTokenType `json:"type"`
	jwt.RegisteredClaims
}

func (c *AuthClaims) Validate(email string, issuer string) error {
	if c.Subject != email {
		return fmt.Errorf("invalid claims: token subject %q does not match %q", c.Subject, email)
	}

	return nil
}
