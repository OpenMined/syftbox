package syftmsg

import (
	"encoding/json"
	"fmt"
	"time"

	"encoding/base64"

	"github.com/google/uuid"
	"github.com/openmined/syftbox/internal/utils"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error in field %s: %s", e.Field, e.Message)
}

// SyftMethod represents the HTTP method in the Syft protocol
type SyftMethod string

// IsValid checks if the method is valid
func (m SyftMethod) IsValid() bool {
	switch m {
	case MethodGET, MethodPOST, MethodPUT, MethodDELETE:
		return true
	default:
		return false
	}
}

// Validate validates the method
func (m SyftMethod) Validate() error {
	if m == "" {
		return nil
	}
	if !m.IsValid() {
		return &ValidationError{
			Field:   "method",
			Message: fmt.Sprintf("invalid method: %s", m),
		}
	}
	return nil
}

// SyftStatus represents the status code in the Syft protocol
type SyftStatus int

// IsValid checks if the status code is valid
func (s SyftStatus) IsValid() bool {
	return s >= 100 && s <= 599
}

func (s SyftStatus) isDefined() bool {
	return s != 0
}

// Validate validates the status code
func (s SyftStatus) Validate() error {
	if !s.isDefined() {
		return nil
	}
	if !s.IsValid() {
		return &ValidationError{
			Field:   "status_code",
			Message: fmt.Sprintf("invalid status code: %d", s),
		}
	}
	return nil
}

const (
	// DefaultMessageExpiry is the default time in seconds before a message expires
	// 1 day
	DefaultMessageExpiry = 24 * 60 * 60 * time.Second

	// HTTP Methods
	MethodGET    SyftMethod = "GET"
	MethodPOST   SyftMethod = "POST"
	MethodPUT    SyftMethod = "PUT"
	MethodDELETE SyftMethod = "DELETE"

	// Status codes
	StatusOK SyftStatus = 200
)

// SyftMessage represents a base message for Syft protocol communication
type SyftRPCMessage struct {
	// ID is the unique identifier of the message
	ID uuid.UUID `json:"id"`

	// Sender is the sender of the message
	Sender string `json:"sender"`

	// URL is the URL of the message
	URL utils.SyftBoxURL `json:"url"`

	// Body is the body of the message in bytes
	Body []byte `json:"body,omitempty"`

	// Headers contains additional headers for the message
	Headers map[string]string `json:"headers"`

	// Created is the timestamp when the message was created
	Created time.Time `json:"created"`

	// Expires is the timestamp when the message expires
	Expires time.Time `json:"expires"`

	Method SyftMethod `json:"method,omitempty"`

	StatusCode SyftStatus `json:"status_code,omitempty"`
}

// NewSyftMessage creates a new SyftMessage with default values
func NewSyftRPCMessage(
	sender string, url utils.SyftBoxURL, method SyftMethod, body []byte, headers map[string]string,
) (*SyftRPCMessage, error) {

	created_at := time.Now().UTC()

	// Timezone is UTC by default for SyftRPC messages

	msg := &SyftRPCMessage{
		ID:      uuid.New(),
		Sender:  sender,
		URL:     url,
		Body:    body,
		Headers: headers,
		Created: created_at,
		Expires: created_at.Add(time.Duration(DefaultMessageExpiry)),
		Method:  method,
	}

	if err := msg.Validate(); err != nil {
		return nil, err
	}

	return msg, nil
}

// MarshalJSON implements custom JSON marshaling to handle bytes as base64
func (m *SyftRPCMessage) MarshalJSON() ([]byte, error) {
	type Alias SyftRPCMessage
	return json.Marshal(&struct {
		*Alias
		URL  string `json:"url"`
		Body string `json:"body,omitempty"`
	}{
		Alias: (*Alias)(m),
		URL:   m.URL.String(),
		Body:  base64.URLEncoding.EncodeToString(m.Body),
	})
}

// JSONString returns a properly formatted JSON string with decoded body
func (m *SyftRPCMessage) ToJsonMap() map[string]interface{} {
	var bodyContent interface{}
	if err := json.Unmarshal(m.Body, &bodyContent); err != nil {
		bodyContent = string(m.Body)
	}

	return map[string]interface{}{
		"id":          m.ID,
		"sender":      m.Sender,
		"url":         m.URL.String(),
		"headers":     m.Headers,
		"created":     m.Created,
		"expires":     m.Expires,
		"method":      m.Method,
		"status_code": m.StatusCode,
		"body":        bodyContent,
	}
}

// Validate validates the message
func (m *SyftRPCMessage) Validate() error {
	if m.ID == uuid.Nil {
		return &ValidationError{
			Field:   "id",
			Message: "id cannot be empty",
		}
	}
	if m.Sender == "" {
		return &ValidationError{
			Field:   "sender",
			Message: "sender cannot be empty",
		}
	}
	if err := m.URL.Validate(); err != nil {
		return err
	}

	// If Method is defined, validate it
	if err := m.Method.Validate(); err != nil {
		return err
	}

	// If StatusCode is defined, validate it
	if err := m.StatusCode.Validate(); err != nil {
		return err
	}
	return nil
}
