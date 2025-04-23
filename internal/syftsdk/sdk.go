package syftsdk

import (
	"io"
	"time"

	"github.com/bytedance/sonic"
	"resty.dev/v3"
)

// SyftSDK is the main client for interacting with the Syft API
type SyftSDK struct {
	client   *resty.Client
	baseURL  string
	Datasite *DatasiteAPI
	Blob     *BlobAPI
	Events   *EventsAPI
}

// New creates a new SyftSDK client
func New(baseURL string) (*SyftSDK, error) {
	client := resty.New().
		SetBaseURL(baseURL).
		SetRetryCount(3).
		SetRetryWaitTime(1*time.Second).
		SetRetryMaxWaitTime(5*time.Second).
		AddContentTypeEncoder("json", func(w io.Writer, v any) error {
			return sonic.ConfigDefault.NewEncoder(w).Encode(v)
		}).
		AddContentTypeDecoder("json", func(r io.Reader, v any) error {
			return sonic.ConfigDefault.NewDecoder(r).Decode(v)
		})

	datasiteAPI := newDatasiteAPI(client)
	blobAPI := newBlobAPI(client)
	eventsAPI := newEventsAPI(baseURL)

	return &SyftSDK{
		client:   client,
		baseURL:  baseURL,
		Datasite: datasiteAPI,
		Blob:     blobAPI,
		Events:   eventsAPI,
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
	if s.Events != nil {
		s.Events.SetUser(user)
	}
	return nil
}
