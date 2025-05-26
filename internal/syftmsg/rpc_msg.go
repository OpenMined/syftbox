package syftmsg

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"encoding/base64"

	"github.com/google/uuid"
)

// SyftMethod represents the HTTP method in the Syft protocol
type SyftMethod string

// SyftStatus represents the status code in the Syft protocol
type SyftStatus int

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

type SyftBoxURL struct {
	Datasite string `json:"datasite"`
	AppName  string `json:"app_name"`
	Endpoint string `json:"endpoint"`
}

func (u *SyftBoxURL) String() string {
	// Clean the endpoint to remove any leading/trailing slashes
	endpoint := strings.Trim(u.Endpoint, "/")
	// format: "syft://{datasite}/app_data/{app_name}/rpc/{endpoint}"
	return fmt.Sprintf("syft://%s/app_data/%s/rpc/%s", u.Datasite, u.AppName, endpoint)
}

func NewSyftBoxURL(datasite, appName, endpoint string) *SyftBoxURL {
	return &SyftBoxURL{
		Datasite: datasite,
		AppName:  appName,
		Endpoint: endpoint,
	}
}

func FromSyftURL(url string) (*SyftBoxURL, error) {

	if !strings.HasPrefix(url, "syft://") {
		return nil, fmt.Errorf("invalid syft url: %s", url)
	}

	// remove the syft:// prefix
	url = strings.TrimPrefix(url, "syft://")

	// split the url into parts
	parts := strings.Split(url, "/")

	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid syft url: %s", url)
	}

	// Extract components
	datasite := parts[0]
	appName := parts[2]
	endpoint := strings.Join(parts[4:], "/")

	return NewSyftBoxURL(datasite, appName, endpoint), nil
}

func (u *SyftBoxURL) ToLocalPath() string {
	// Clean the endpoint to remove any leading/trailing slashes
	endpoint := strings.Trim(u.Endpoint, "/")
	return filepath.Join(u.Datasite, "app_data", u.AppName, "rpc", endpoint)
}

// SyftMessage represents a base message for Syft protocol communication
type SyftRPCMessage struct {
	// ID is the unique identifier of the message
	ID uuid.UUID `json:"id"`

	// Sender is the sender of the message
	Sender string `json:"sender"`

	// URL is the URL of the message
	URL SyftBoxURL `json:"url"`

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
func NewSyftRPCMessage(httpMsg HttpMsg) *SyftRPCMessage {
	now := time.Now().UTC()
	headers := httpMsg.Headers
	if headers == nil {
		headers = make(map[string]string)
	}
	return &SyftRPCMessage{
		ID:         uuid.MustParse(httpMsg.Id),
		Sender:     httpMsg.From,
		URL:        *NewSyftBoxURL(httpMsg.To, httpMsg.AppName, httpMsg.AppEp),
		Body:       httpMsg.Body,
		Headers:    headers,
		Created:    now,
		Expires:    now.Add(time.Duration(DefaultMessageExpiry)),
		Method:     SyftMethod(httpMsg.Method),
		StatusCode: SyftStatus(httpMsg.Status),
	}
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
	url, err := FromSyftURL(aux.URL)
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
