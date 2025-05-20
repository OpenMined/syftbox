package utils

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
)

var (
	ErrInvalidURL = errors.New("invalid url")
	regexURL      = regexp.MustCompile(`^https?://`)
)

func GetFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func ValidateURL(urlString string) error {
	if urlString == "" {
		return fmt.Errorf("%w '%s'", ErrInvalidURL, urlString)
	} else if _, err := url.ParseRequestURI(urlString); err != nil {
		return fmt.Errorf("%w '%s'", ErrInvalidURL, urlString)
	} else if !regexURL.MatchString(urlString) {
		return fmt.Errorf("%w '%s'", ErrInvalidURL, urlString)
	}
	return nil
}

func IsValidURL(urlString string) bool {
	return ValidateURL(urlString) == nil
}
