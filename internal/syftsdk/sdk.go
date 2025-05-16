package syftsdk

import (
	"time"

	"github.com/openmined/syftbox/internal/version"
	"resty.dev/v3"
)

// SyftSDK is the main client for interacting with the Syft API
type SyftSDK struct {
	client   *resty.Client
	baseURL  string
	Datasite *DatasiteAPI
	Blob     *BlobAPI
	Events   *EventsAPI
	SendMsg  *SendMsgAPI
}

// New creates a new SyftSDK client
func New(baseURL string) (*SyftSDK, error) {
	client := resty.New().
		SetBaseURL(baseURL).
		SetRetryCount(3).
		SetRetryWaitTime(1*time.Second).
		SetHeader(HeaderUserAgent, "SyftBox/"+version.Version).
		SetHeader(HeaderSyftVersion, version.Version).
		SetRetryMaxWaitTime(5*time.Second).
		AddContentTypeEncoder("json", jsonEncoder).
		AddContentTypeDecoder("json", jsonDecoder)

	datasiteAPI := newDatasiteAPI(client)
	blobAPI := newBlobAPI(client)
	sendMsgAPI := NewSendMsgAPI(client)
	eventsAPI := newEventsAPI(baseURL)

	return &SyftSDK{
		client:   client,
		baseURL:  baseURL,
		Datasite: datasiteAPI,
		Blob:     blobAPI,
		Events:   eventsAPI,
		SendMsg:  sendMsgAPI,
	}, nil
}

// Close terminates all connections and cleans up resources
func (s *SyftSDK) Close() {
	if s.Events.IsConnected() {
		s.Events.Close()
	}
	s.client.Close()
}

// Login sets the user authentication for API calls and events
func (s *SyftSDK) Login(user string) error {
	s.client.SetQueryParam("user", user) // todo remove with auth
	s.client.SetHeader(HeaderSyftUser, user)
	if s.Events != nil {
		s.Events.SetUser(user)
	}
	return nil
}
