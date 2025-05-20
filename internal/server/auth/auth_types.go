package auth

import (
	"errors"

	_ "embed"

	"github.com/openmined/syftbox/internal/utils"
)

//go:embed authmail.html.tmpl
var emailTemplate string

var (
	ErrInvalidEmail        = utils.ErrInvalidEmail
	ErrInvalidOTP          = errors.New("invalid otp")
	ErrInvalidToken        = errors.New("invalid token")
	ErrInvalidRequestToken = errors.New("invalid request token")
	ErrInvalidAccessToken  = errors.New("invalid access token")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)
