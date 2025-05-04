package auth

import (
	"crypto/rand"
	"fmt"
	"net/mail"
)

const table = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ" // base34 table

func generateOTP(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be greater than 0")
	}

	result := make([]byte, length)
	if _, err := rand.Read(result); err != nil {
		return "", err
	}

	for i := range result {
		result[i] = table[result[i]%byte(len(table))]
	}

	return string(result), nil
}

func validEmail(email string) bool {
	if email == "" {
		return false
	}

	_, err := mail.ParseAddress(email)
	return err == nil
}
