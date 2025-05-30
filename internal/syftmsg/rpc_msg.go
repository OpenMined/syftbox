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
func NewSyftRPCMessage(httpMsg HttpMsg) (*SyftRPCMessage, error) {

	// Timezone is UTC by default for SyftRPC messages
	now := time.Now().UTC()

	headers := httpMsg.Headers
	if headers == nil {
		headers = make(map[string]string)
	}

	msg := &SyftRPCMessage{
		ID:      uuid.MustParse(httpMsg.Id),
		Sender:  httpMsg.From,
		URL:     httpMsg.SyftURL,
		Body:    httpMsg.Body,
		Headers: headers,
		Created: now,
		Expires: now.Add(time.Duration(DefaultMessageExpiry)),
		Method:  SyftMethod(httpMsg.Method),
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

// UnmarshalJSON implements custom JSON unmarshaling
func (m *SyftRPCMessage) UnmarshalJSON(data []byte) error {
	type Alias struct {
		ID         uuid.UUID         `json:"id"`
		Sender     string            `json:"sender"`
		URL        string            `json:"url"`
		Body       string            `json:"body,omitempty"`
		Headers    map[string]string `json:"headers"`
		Created    time.Time         `json:"created"`
		Expires    time.Time         `json:"expires"`
		Method     SyftMethod        `json:"method,omitempty"`
		StatusCode SyftStatus        `json:"status_code,omitempty"`
	}

	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Parse URL
	url, err := utils.FromSyftURL(aux.URL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	// Set fields
	m.ID = aux.ID
	m.Sender = aux.Sender
	m.URL = *url
	m.Headers = aux.Headers
	m.Created = aux.Created
	m.Expires = aux.Expires
	m.Method = aux.Method
	m.StatusCode = aux.StatusCode

	// Handle body
	if aux.Body != "" {
		if body, err := base64.URLEncoding.DecodeString(aux.Body); err == nil {
			m.Body = body
		} else {
			m.Body = []byte(aux.Body)
		}
	}

	// Validate the message
	if err := m.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	return nil
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
