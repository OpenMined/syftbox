package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestToken(subject, issuer, secret string, expiry time.Duration, tokenType AuthTokenType) (string, error) {
	var expiryTime *jwt.NumericDate
	if expiry > 0 {
		expiryTime = jwt.NewNumericDate(time.Now().Add(expiry))
	}
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    issuer,
			ExpiresAt: expiryTime,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Type: tokenType,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func TestParseClaims_ValidToken(t *testing.T) {
	secret := "test-secret"
	token, err := createTestToken("user123", "issuer", secret, time.Minute, AccessToken)
	assert.NoError(t, err)

	claims, err := ParseClaims(token, secret)
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "user123", claims.Subject)
	assert.Equal(t, AccessToken, claims.Type)
	assert.Equal(t, "issuer", claims.Issuer)
}

func TestParseClaims_InvalidToken(t *testing.T) {
	secret := "test-secret"
	_, err := ParseClaims("invalid.token.string", secret)
	assert.Error(t, err)
}

func TestParseClaims_WrongSecret(t *testing.T) {
	secret := "test-secret"
	token, err := createTestToken("user123", "issuer", secret, time.Minute, AccessToken)
	require.NoError(t, err)
	require.NotNil(t, token)

	_, err = ParseClaims(token, "wrong-secret")
	assert.Error(t, err)
}
