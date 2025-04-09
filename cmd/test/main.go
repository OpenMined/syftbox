package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func main() {
	parts := strings.FieldsFunc(filepath.Clean("/path/to/potato"), func(r rune) bool {
		return r == '/'
	})

	fmt.Printf("parts: %v\n", parts)

	parts = strings.Split("/path/to/potato", "/")
	fmt.Printf("parts: %v\n", parts)
}
