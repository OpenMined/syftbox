package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func GenerateTokens(subject string, config *Config) (accessToken string, refreshToken string, err error) {
	accessToken, err = NewAccessToken(subject, config.TokenIssuer, config.AccessTokenSecret, config.AccessTokenExpiry)
	if err != nil {
		return "", "", err
	}

	// todo save it in a db?

	refreshToken, err = NewRefreshToken(subject, config.TokenIssuer, config.RefreshTokenSecret, config.RefreshTokenExpiry)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func NewAccessToken(subject, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return NewToken(subject, issuer, jwtSecret, expiry, AccessToken)
}

func NewRefreshToken(subject, issuer, jwtSecret string, expiry time.Duration) (string, error) {
	return NewToken(subject, issuer, jwtSecret, expiry, RefreshToken)
}

func NewToken(subject, issuer, jwtSecret string, expiry time.Duration, tokenType AuthTokenType) (string, error) {
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
