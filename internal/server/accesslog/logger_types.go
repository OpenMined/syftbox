package accesslog

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/acl"
)

const (
	MaxLogSize        = 10 * 1024 * 1024 // 10MB
	MaxLogFiles       = 5
	LogFilePermission = 0600
	LogDirPermission  = 0700
)

type AccessType string

const (
	AccessTypeAdmin AccessType = "admin"
	AccessTypeRead  AccessType = "read"
	AccessTypeWrite AccessType = "write"
	AccessTypeDeny  AccessType = "deny"
)

type AccessLogEntry struct {
	Timestamp    time.Time  `json:"timestamp"`
	Path         string     `json:"path"`
	AccessType   AccessType `json:"access_type"`
	User         string     `json:"user"`
	IP           string     `json:"ip"`
	UserAgent    string     `json:"user_agent"`
	Method       string     `json:"method"`
	StatusCode   int        `json:"status_code"`
	Allowed      bool       `json:"allowed"`
	DeniedReason string     `json:"denied_reason,omitempty"`
}

// MarshalJSON customizes the JSON output for AccessLogEntry
func (e AccessLogEntry) MarshalJSON() ([]byte, error) {
	// Use an anonymous struct that mirrors our fields but with formatted timestamp
	return json.Marshal(&struct {
		Timestamp    string     `json:"timestamp"`
		Path         string     `json:"path"`
		AccessType   AccessType `json:"access_type"`
		User         string     `json:"user"`
		IP           string     `json:"ip"`
		UserAgent    string     `json:"user_agent"`
		Method       string     `json:"method"`
		StatusCode   int        `json:"status_code"`
		Allowed      bool       `json:"allowed"`
		DeniedReason string     `json:"denied_reason,omitempty"`
	}{
		Timestamp:    e.Timestamp.Format("2006-01-02 15:04:05.000 UTC"),
		Path:         e.Path,
		AccessType:   e.AccessType,
		User:         e.User,
		IP:           e.IP,
		UserAgent:    e.UserAgent,
		Method:       e.Method,
		StatusCode:   e.StatusCode,
		Allowed:      e.Allowed,
		DeniedReason: e.DeniedReason,
	})
}

// UnmarshalJSON customizes the JSON parsing for AccessLogEntry
func (e *AccessLogEntry) UnmarshalJSON(data []byte) error {
	// Use an anonymous struct that mirrors our fields but with string timestamp
	aux := &struct {
		Timestamp    string     `json:"timestamp"`
		Path         string     `json:"path"`
		AccessType   AccessType `json:"access_type"`
		User         string     `json:"user"`
		IP           string     `json:"ip"`
		UserAgent    string     `json:"user_agent"`
		Method       string     `json:"method"`
		StatusCode   int        `json:"status_code"`
		Allowed      bool       `json:"allowed"`
		DeniedReason string     `json:"denied_reason,omitempty"`
	}{}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Parse the timestamp string back to time.Time
	t, err := time.Parse("2006-01-02 15:04:05.000 MST", aux.Timestamp)
	if err != nil {
		// Try to parse RFC3339 format for backward compatibility
		t, err = time.Parse(time.RFC3339, aux.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to parse timestamp: %w", err)
		}
	}

	e.Timestamp = t
	e.Path = aux.Path
	e.AccessType = aux.AccessType
	e.User = aux.User
	e.IP = aux.IP
	e.UserAgent = aux.UserAgent
	e.Method = aux.Method
	e.StatusCode = aux.StatusCode
	e.Allowed = aux.Allowed
	e.DeniedReason = aux.DeniedReason

	return nil
}

// AccessLoggerInterface defines the interface for access logging operations
type AccessLoggerInterface interface {
	LogAccess(ctx *gin.Context, path string, accessType AccessType, accessLevel acl.AccessLevel, allowed bool, deniedReason string)
	GetUserLogs(user string, limit int) ([]AccessLogEntry, error)
	Close() error
}

// LogWriter defines the interface for writing log entries
type LogWriter interface {
	WriteEntry(entry AccessLogEntry) error
	Close() error
}
