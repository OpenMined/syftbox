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
	DefaultMessageExpiry = 24 * time.Hour

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
		Expires:    now.Add(DefaultMessageExpiry),
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
		Body:  base64.StdEncoding.EncodeToString(m.Body),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling to handle bytes from base64
func (m *SyftRPCMessage) UnmarshalJSON(data []byte) error {
	type Alias SyftRPCMessage
	aux := &struct {
		*Alias
		URL  string `json:"url"`
		Body string `json:"body,omitempty"`
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	// Parse the URL string into SyftBoxURL
	// Assuming URL format is "syft://{datasite}/app_data/{app_name}/rpc/{endpoint}"
	urlParts := strings.Split(strings.TrimPrefix(aux.URL, "syft://"), "/")
	if len(urlParts) >= 4 {
		m.URL = *NewSyftBoxURL(urlParts[0], urlParts[2], urlParts[3])
	}
	// Decode base64 body
	if aux.Body != "" {
		// decode the body, using URL encoding
		body, err := base64.URLEncoding.DecodeString(aux.Body)
		if err != nil {
			return err
		}
		m.Body = body
	}
	return nil
}
