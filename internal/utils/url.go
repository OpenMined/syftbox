package utils

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// URL scheme and components
	syftScheme    = "syft://"
	appDataPath   = "app_data"
	rpcPath       = "rpc"
	pathSeparator = "/"
)

// SyftBoxURL represents a parsed syft:// URL with its components
type SyftBoxURL struct {
	Datasite    string            `json:"datasite"`
	AppName     string            `json:"app_name"`
	Endpoint    string            `json:"endpoint"`
	QueryParams map[string]string `json:"query_params"`
}

// ValidationError represents a validation error with field context
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error in field '%s': %s", e.Field, e.Message)
}

// Syft base URL
func (s *SyftBoxURL) BaseURL() string {
	endpoint := strings.Trim(s.Endpoint, pathSeparator)
	return fmt.Sprintf("%s%s/%s/%s/%s/%s",
		syftScheme, s.Datasite, appDataPath, s.AppName, rpcPath, endpoint)
}

// String returns the string representation of the SyftBoxURL
func (s *SyftBoxURL) String() string {
	baseURL := s.BaseURL()

	// Add query parameters if they exist
	if len(s.QueryParams) > 0 {
		// Sort query params by key lexicographically to ensure consistent ordering
		sortedKeys := make([]string, 0, len(s.QueryParams))
		for key := range s.QueryParams {
			sortedKeys = append(sortedKeys, key)
		}
		sort.Strings(sortedKeys)

		queryParams := make([]string, 0, len(s.QueryParams))
		for _, key := range sortedKeys {
			value := s.QueryParams[key]
			queryParams = append(queryParams, fmt.Sprintf("%s=%s", key, url.QueryEscape(value)))
		}
		baseURL += "?" + strings.Join(queryParams, "&")
	}

	return baseURL
}

// ToLocalPath converts the SyftBoxURL to a local file system path
func (s *SyftBoxURL) ToLocalPath() string {
	endpoint := strings.Trim(s.Endpoint, pathSeparator)
	return filepath.ToSlash(filepath.Join(s.Datasite, appDataPath, s.AppName, rpcPath, endpoint))
}

// Validate validates the SyftBoxURL fields
func (s *SyftBoxURL) Validate() error {
	if s.Datasite == "" {
		return &ValidationError{
			Field:   "datasite",
			Message: "datasite cannot be empty",
		}
	}

	// Validate datasite follows email pattern
	if !IsValidEmail(s.Datasite) {
		return &ValidationError{
			Field:   "datasite",
			Message: "datasite must be a valid email address",
		}
	}

	if s.AppName == "" {
		return &ValidationError{
			Field:   "app_name",
			Message: "app_name cannot be empty",
		}
	}
	if s.Endpoint == "" {
		return &ValidationError{
			Field:   "endpoint",
			Message: "endpoint cannot be empty",
		}
	}

	// Validate endpoint doesn't contain spaces or special characters
	if strings.ContainsAny(s.Endpoint, " ?&=") {
		return &ValidationError{
			Field:   "endpoint",
			Message: "endpoint cannot contain spaces or special characters (?&=)",
		}
	}

	return nil
}

// UnmarshalParam implements gin.UnmarshalParam for automatic query param binding
func (s *SyftBoxURL) UnmarshalParam(param string) error {
	parsed, err := FromSyftURL(param)
	if err != nil {
		slog.Error("Failed to parse syft url", "error", err, "url", param)
		return err
	}
	*s = *parsed
	return nil
}

func (s *SyftBoxURL) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *SyftBoxURL) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	parsed, err := FromSyftURL(str)
	if err != nil {
		return err
	}
	*s = *parsed
	return nil
}

// NewSyftBoxURL creates a new SyftBoxURL with validation
func NewSyftBoxURL(datasite, appName, endpoint string) (*SyftBoxURL, error) {
	syftURL := &SyftBoxURL{
		Datasite: datasite,
		AppName:  appName,
		Endpoint: endpoint,
	}

	if err := syftURL.Validate(); err != nil {
		return nil, err
	}

	return syftURL, nil
}

// SetQueryParams sets the query parameters
func (s *SyftBoxURL) SetQueryParams(queryParams map[string]string) {
	s.QueryParams = queryParams
}

// parseQueryParams parses and validates query parameters from a URL
func parseQueryParams(rawQuery string) (map[string]string, error) {
	if rawQuery == "" {
		return nil, nil
	}

	queryParams := make(map[string]string)
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query parameters: %w", err)
	}

	for key, values := range values {
		// Validate key doesn't contain spaces or special characters
		if strings.ContainsAny(key, " ?&=") {
			return nil, fmt.Errorf("query parameter key '%s' cannot contain spaces or special characters (?&=)", key)
		}
		// Use the first value if multiple values exist
		if len(values) > 0 {
			queryParams[key] = values[0]
		}
	}

	return queryParams, nil
}

// FromSyftURL parses a syft URL string into a SyftBoxURL struct
func FromSyftURL(rawURL string) (*SyftBoxURL, error) {
	// Parse the URL using standard library
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// Validate scheme
	if parsedURL.Scheme != "syft" {
		return nil, fmt.Errorf("invalid scheme: expected 'syft', got '%s'", parsedURL.Scheme)
	}

	// datasite is the host of the URL + @ + username
	datasite := parsedURL.Host

	if datasite == "" {
		return nil, fmt.Errorf("invalid syft url: missing datasite (host)")
	}

	if parsedURL.User == nil || parsedURL.User.Username() == "" {
		return nil, fmt.Errorf("invalid syft url: invalid datasite name")
	}

	username := parsedURL.User.Username()
	datasite = username + "@" + datasite

	// Split path into components and remove empty strings
	path := strings.Trim(parsedURL.Path, pathSeparator)
	parts := make([]string, 0)
	for _, part := range strings.Split(path, pathSeparator) {
		if part != "" {
			parts = append(parts, part)
		}
	}

	// Validate path structure
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid path: expected format 'app_data/app_name/rpc/endpoint'")
	}

	// Validate expected structure
	if parts[0] != appDataPath {
		return nil, fmt.Errorf("invalid path: expected '%s' at position 1", appDataPath)
	}

	// Find the index of rpc in the path
	rpcIndex := -1
	for i, part := range parts {
		if part == rpcPath {
			rpcIndex = i
			break
		}
	}

	if rpcIndex == -1 {
		return nil, fmt.Errorf("invalid path: expected '%s' in path", rpcPath)
	}

	// Extract components
	appName := strings.Join(parts[1:rpcIndex], pathSeparator)
	endpoint := strings.Join(parts[rpcIndex+1:], pathSeparator)

	// Create SyftBoxURL
	syftURL, err := NewSyftBoxURL(datasite, appName, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create syft url from components: %w", err)
	}

	// Validate query params
	queryParams, err := parseQueryParams(parsedURL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query parameters: %w", err)
	}

	// Set query params
	syftURL.SetQueryParams(queryParams)

	return syftURL, nil
}
