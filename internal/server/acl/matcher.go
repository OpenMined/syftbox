package acl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

type MatchContext any // i don't know what all we'll need, so just putting it as any

type MatcherType int

const (
	MatcherTypeExact MatcherType = iota
	MatcherTypeGlob
	MatcherTypeTemplate
)

type Matcher interface {
	Match(path string, ctx MatchContext) (bool, error)
	Type() MatcherType
}

// ----------------------------------------------------------------------------

type ExactMatcher struct {
	Value string
}

func newExactMatcher(value string) *ExactMatcher {
	return &ExactMatcher{Value: value}
}

func (e *ExactMatcher) Match(path string, ctx MatchContext) (bool, error) {
	return e.Value == path, nil
}

func (e *ExactMatcher) Type() MatcherType {
	return MatcherTypeExact
}

// ----------------------------------------------------------------------------

type GlobMatcher struct {
	pattern string // e.g. "*", "**", "proj*"
}

func newGlobMatcher(pattern string) *GlobMatcher {
	return &GlobMatcher{pattern: pattern}
}

func (g *GlobMatcher) Match(path string, ctx MatchContext) (bool, error) {
	return doublestar.Match(g.pattern, path)
}

func (g *GlobMatcher) Type() MatcherType {
	return MatcherTypeGlob
}

func hasGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// ----------------------------------------------------------------------------

var tplDataPool = sync.Pool{
	New: func() any {
		return &templateData{}
	},
}

var tplFuncMap = template.FuncMap{
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

func getUserHash(userID string) string {
	hash := sha256.Sum256([]byte(userID))
	hashStr := hex.EncodeToString(hash[:8]) // 8 bytes = 16 hex chars!
	return hashStr
}

// templateData holds variables available for template resolution
type templateData struct {
	UserEmail string
	UserHash  string
	Year      string
	Month     string
	Date      string
}

func newTemplateData(userID string) *templateData {
	now := time.Now().UTC()
	vars := tplDataPool.Get().(*templateData)
	vars.UserEmail = userID
	vars.UserHash = getUserHash(userID)
	vars.Year = fmt.Sprintf("%04d", now.Year())
	vars.Month = fmt.Sprintf("%02d", now.Month())
	vars.Date = fmt.Sprintf("%02d", now.Day())
	return vars
}

func putTemplateData(vars *templateData) {
	tplDataPool.Put(vars)
}

type TemplateMatcher struct {
	tpl *template.Template
}

func newTemplateMatcher(tplString string) (*TemplateMatcher, error) {
	tpl, err := template.New("path").Funcs(tplFuncMap).Parse(tplString)
	if err != nil {
		return nil, fmt.Errorf("template matcher parse: %w", err)
	}

	return &TemplateMatcher{tpl: tpl}, nil
}

func (t *TemplateMatcher) Match(path string, ctx MatchContext) (bool, error) {
	// Execute the template to get the resolved path
	tplData := newTemplateData(ctx.(*User).ID)
	defer putTemplateData(tplData)

	var buf strings.Builder
	if err := t.tpl.Execute(&buf, tplData); err != nil {
		return false, fmt.Errorf("failed to execute template: %w", err)
	}

	resolvedTpl := buf.String()
	inner, err := matcherFromResolvedPattern(resolvedTpl)
	if err != nil {
		return false, fmt.Errorf("failed to create inner matcher: %w", err)
	}

	return inner.Match(path, ctx)
}

func (t *TemplateMatcher) Type() MatcherType {
	return MatcherTypeTemplate
}

func hasTemplatePattern(pattern string) bool {
	return strings.Contains(pattern, "{{") && strings.Contains(pattern, "}}")
}

// ----------------------------------------------------------------------------

func matcherFromPattern(path string) (Matcher, error) {
	if hasTemplatePattern(path) {
		return newTemplateMatcher(path)
	}

	return matcherFromResolvedPattern(path)
}

func matcherFromResolvedPattern(path string) (Matcher, error) {
	if hasGlobPattern(path) {
		return newGlobMatcher(path), nil
	}

	return newExactMatcher(path), nil
}
