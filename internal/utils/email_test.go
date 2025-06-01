package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		error bool
	}{
		{name: "valid-email", email: "test@example.com", error: false},
		{name: "valid-email-with-plus", email: "test+test@example.com", error: false},
		{name: "valid-email-with-dot-in-username", email: "test.test@example.com", error: false},
		{name: "valid-email-with-dash-in-username", email: "test-test@example.com", error: false},
		{name: "valid-email-with-dot-in-domain", email: "test@example.co.in", error: false},
		{name: "valid-email-with-dash-in-domain", email: "test@example-domain.com", error: false},
		{name: "invalid-email", email: "123", error: true},
		{name: "invalid-email-no-tld", email: "test@example", error: true},
		{name: "invalid-email-no-at", email: "testexample.com", error: true},
		{name: "invalid-email-no-username", email: "@example.com", error: true},
		{name: "invalid-email-empty", email: "", error: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateEmail(test.email)
			if test.error {
				assert.Error(t, err, test.name)
			} else {
				assert.NoError(t, err, test.name)
			}
		})
	}
}
