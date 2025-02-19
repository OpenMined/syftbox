package utils

import (
	crytoRand "crypto/rand"
	"encoding/hex"
)

func TokenHex(len int) string {
	b := make([]byte, len)
	_, err := crytoRand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
