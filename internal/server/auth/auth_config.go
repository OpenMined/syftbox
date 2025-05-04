package auth

import "time"

type Config struct {
	Enabled            bool
	TokenIssuer        string
	RefreshTokenSecret string
	RefreshTokenExpiry time.Duration
	AccessTokenSecret  string
	AccessTokenExpiry  time.Duration
	EmailOTPLength     int
	EmailOTPExpiry     time.Duration
}
