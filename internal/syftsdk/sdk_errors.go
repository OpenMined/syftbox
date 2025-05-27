package syftsdk

import "errors"

// sdk common
var (
	ErrNoRefreshToken = errors.New("sdk: refresh token missing")
	ErrNoServerURL    = errors.New("sdk: server url missing")
	ErrInvalidEmail   = errors.New("sdk: invalid email")
)

// auth
var (
	ErrInvalidOTP = errors.New("sdk: invalid otp")
)

// blob
var (
	ErrNoPermissions = errors.New("no permissions")
)
