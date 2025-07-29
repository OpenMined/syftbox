package acl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
	"time"
)

var funcMap = template.FuncMap{
	"sha2": func(s string, n ...uint8) string {
		hash := sha256.Sum256([]byte(s))
		hashStr := hex.EncodeToString(hash[:])

		if len(n) > 0 {
			length := min(n[0], 64)
			if length <= 0 {
				length = 16
			}
			return hashStr[:length]
		}

		return hashStr
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
}

// TemplateContext holds variables available for template resolution
type TemplateContext struct {
	UserEmail string
	UserHash  string
	Year      string
	Month     string
	Date      string
}

// NewTemplateContext creates a template context for the given user
func NewTemplateContext(userID string) *TemplateContext {
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(userID))

	return &TemplateContext{
		UserEmail: userID,
		UserHash:  fmt.Sprintf("%x", hash)[:16],
		Year:      fmt.Sprintf("%04d", now.Year()),
		Month:     fmt.Sprintf("%02d", now.Month()),
		Date:      fmt.Sprintf("%02d", now.Day()),
	}
}

func NewTemplatePattern(pattern string) (*template.Template, error) {
	// bind functions or whatever to the template here!
	tmpl := template.New("aclPattern").Funcs(funcMap)
	return tmpl.Parse(pattern)
}

func HasTemplatePattern(pattern string) bool {
	return strings.Contains(pattern, "{{") && strings.Contains(pattern, "}}")
}
