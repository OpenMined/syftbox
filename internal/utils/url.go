package utils

import (
	"fmt"
	"net/url"
	"path/filepath"
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
	queryParams map[string]string // private field to store query params
}

// ValidationError represents a validation error with field context
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error in field '%s': %s", e.Field, e.Message)
}

// QueryParams returns a copy of the query parameters
func (s *SyftBoxURL) QueryParams() map[string]string {
	if s.queryParams == nil {
		return make(map[string]string)
	}

	// Return a copy to prevent external modification
	result := make(map[string]string, len(s.queryParams))
	for k, v := range s.queryParams {
		result[k] = v
	}
	return result
}

// SetQueryParams sets the query parameters
func (s *SyftBoxURL) SetQueryParams(params map[string]string) {
	if params == nil {
		s.queryParams = nil
		return
	}

	// Create a copy to prevent external modification
	s.queryParams = make(map[string]string, len(params))
	for k, v := range params {
		s.queryParams[k] = v
	}
}

// String returns the string representation of the SyftBoxURL
func (s *SyftBoxURL) String() string {
	endpoint := strings.Trim(s.Endpoint, pathSeparator)
	baseURL := fmt.Sprintf("%s%s/%s/%s/%s/%s",
		syftScheme, s.Datasite, appDataPath, s.AppName, rpcPath, endpoint)

	// Add query parameters if they exist
	if len(s.queryParams) > 0 {
		params := make([]string, 0, len(s.queryParams))
		for key, value := range s.queryParams {
			params = append(params, fmt.Sprintf("%s=%s",
				url.QueryEscape(key), url.QueryEscape(value)))
		}
		baseURL += "?" + strings.Join(params, "&")
	}

	return baseURL
}

// ToLocalPath converts the SyftBoxURL to a local file system path
func (s *SyftBoxURL) ToLocalPath() string {
	endpoint := strings.Trim(s.Endpoint, pathSeparator)
	return filepath.Join(s.Datasite, appDataPath, s.AppName, rpcPath, endpoint)
}

// Validate validates the SyftBoxURL fields
func (s *SyftBoxURL) Validate() error {
	if s.Datasite == "" {
		return &ValidationError{
			Field:   "datasite",
			Message: "datasite cannot be empty",
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

// UnmarshalText implements encoding.TextUnmarshaler for automatic binding
func (s *SyftBoxURL) UnmarshalText(text []byte) error {
	parsed, err := FromSyftURL(string(text))
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

	// Use Host for datasite
	datasite := parsedURL.Host
	if datasite == "" {
		return nil, fmt.Errorf("invalid syft url: missing datasite (host)")
	}

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
	if parts[2] != rpcPath {
		return nil, fmt.Errorf("invalid path: expected '%s' at position 3", rpcPath)
	}

	// Extract components
	appName := parts[1]
	endpoint := strings.Join(parts[3:], pathSeparator)

	// Create SyftBoxURL
	syftURL, err := NewSyftBoxURL(datasite, appName, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create syft url from components: %w", err)
	}

	// Parse query parameters
	queryParams, err := parseQueryParams(parsedURL.RawQuery)
	if err != nil {
		return nil, err
	}
	if queryParams != nil {
		syftURL.SetQueryParams(queryParams)
	}

	return syftURL, nil
}
