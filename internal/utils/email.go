package utils

import (
	"errors"
	"net/mail"
	"regexp"
)

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

var (
	ErrEmailEmpty   = errors.New("`email` is empty")
	ErrEmailInvalid = errors.New("`email` is not valid")
)

func ValidateEmail(email string) error {
	if email == "" {
		return ErrEmailEmpty
	}

	// this helps parse emails, but this implements RFC 5322 which allows example@value
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrEmailInvalid
	}

	// this is a fail safe for the above
	if !emailRegex.MatchString(email) {
		return ErrEmailInvalid
	}

	return nil
}
