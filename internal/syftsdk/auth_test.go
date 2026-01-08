package syftsdk

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidOTP(t *testing.T) {
	assert.True(t, IsValidOTP("ABCD1234"))
	assert.False(t, IsValidOTP("abcd1234"), "lowercase should fail")
	assert.False(t, IsValidOTP("ABC123"), "wrong length should fail")
	assert.False(t, IsValidOTP("ABCD123!"), "non-alnum should fail")
}

func TestParseToken_TypeAndExpiry(t *testing.T) {
	now := time.Now()
	claims := &AuthClaims{
		Type: AccessToken,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "alice@example.com",
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		},
	}
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("k"))
	require.NoError(t, err)

	parsed, err := ParseToken(tokenStr, AccessToken)
	require.NoError(t, err)
	assert.Equal(t, AccessToken, parsed.Type)

	_, err = ParseToken(tokenStr, RefreshToken)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token type")

	expiredClaims := &AuthClaims{
		Type: RefreshToken,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "alice@example.com",
			ExpiresAt: jwt.NewNumericDate(now.Add(-10 * time.Minute)),
		},
	}
	expiredStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString([]byte("k"))
	require.NoError(t, err)
	_, err = ParseToken(expiredStr, RefreshToken)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestRefreshAuthTokens_InputValidation(t *testing.T) {
	_, err := RefreshAuthTokens(t.Context(), "not-a-url", "tok")
	assert.ErrorIs(t, err, ErrNoServerURL)

	_, err = RefreshAuthTokens(t.Context(), "http://127.0.0.1:8080", "")
	assert.ErrorIs(t, err, ErrNoRefreshToken)
}

func TestRequestVerifyEmail_InputValidation(t *testing.T) {
	err := RequestEmailCode(t.Context(), "not-a-url", "alice@example.com")
	assert.ErrorIs(t, err, ErrNoServerURL)

	err = RequestEmailCode(t.Context(), "http://127.0.0.1:8080", "bad")
	assert.ErrorIs(t, err, ErrInvalidEmail)

	_, err = VerifyEmailCode(t.Context(), "not-a-url", &VerifyEmailCodeRequest{
		Email: "alice@example.com",
		Code:  "ABCD1234",
	})
	assert.ErrorIs(t, err, ErrNoServerURL)

	_, err = VerifyEmailCode(t.Context(), "http://127.0.0.1:8080", &VerifyEmailCodeRequest{
		Email: "alice@example.com",
		Code:  "bad",
	})
	assert.ErrorIs(t, err, ErrInvalidOTP)
}

