package utils

import (
	"crypto/rand"
	"fmt"
)

const base34Table = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ" // base34 table
const tableLen = byte(len(base34Table))

// RandBase34 generates a random base34 string of the given length
func RandBase34(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid length: %d", length)
	}

	randBytes := make([]byte, length)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	for i := range randBytes {
		randBytes[i] = base34Table[randBytes[i]%tableLen]
	}

	return string(randBytes), nil
}
