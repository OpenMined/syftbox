package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type AuthTokenType string

const (
	AccessToken  AuthTokenType = "access"
	RefreshToken AuthTokenType = "refresh"
)

type Claims struct {
	Type AuthTokenType `json:"type"`
	jwt.RegisteredClaims
}

func ParseClaims(tokenString, jwtSecret string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}
