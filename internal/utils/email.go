package utils

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
)

var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

var (
	ErrInvalidEmail = errors.New("invalid email")
)

const GuestEmail = "guest@syftbox.net"
const GuestEmailLegacy = "guest@syft.org"

func IsValidEmail(email string) bool {
	return ValidateEmail(email) == nil
}

func ValidateEmail(email string) error {
	// this helps parse emails, but this implements RFC 5322 which allows example@value
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("%w %q", ErrInvalidEmail, email)
	}

	// this is a fail safe for the above
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("%w %q", ErrInvalidEmail, email)
	}

	return nil
}
