package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yashgorana/syftbox-go/pkg/message"
	"resty.dev/v3"
)

const (
	v1Files         = "/api/v1/datasite/view"
	v1FilesDownload = "/api/v1/datasite/download"
	v1Events        = "/api/v1/events"
)

type SyftAPI struct {
	client    *resty.Client
	wsManager *WebSocketManager
	baseUrl   string
	auth      string
}

func NewSyftAPI(baseUrl string) (*SyftAPI, error) {
	client := resty.New().
		SetBaseURL(baseUrl).
		SetRetryCount(3).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	wsManager := NewWebSocketManager(baseUrl + v1Events)

	return &SyftAPI{
		client:    client,
		baseUrl:   baseUrl,
		wsManager: wsManager,
	}, nil
}

func (s *SyftAPI) Close() {
	s.wsManager.Close()
	s.client.Close()
}

func (s *SyftAPI) Login(user string) error {
	s.auth = user
	s.wsManager.SetUser(user)
	return nil
}

func (s *SyftAPI) GetDatasiteView(ctx context.Context) (*GetDatasiteViewOutput, error) {
	if s.auth == "" {
		return nil, errors.New("userID is required")
	}

	res, err := s.client.R().
		SetQueryParam("user", s.auth). // todo remove with auth
		SetResult(&GetDatasiteViewOutput{}).
		SetError(&SyftAPIError{}).
		SetContext(ctx).
		Get(v1Files)

	if err != nil {
		return nil, err
	} else if res.IsError() {
		return nil, fmt.Errorf("error: %s", res.Error().(*SyftAPIError).Error)
	}

	return res.Result().(*GetDatasiteViewOutput), nil
}

func (s *SyftAPI) GetFileUrls(ctx context.Context, input *GetFileURLInput) (*GetFileURLOutput, error) {
	if s.auth == "" {
		return nil, errors.New("userID is required")
	}

	res, err := s.client.R().
		SetQueryParam("user", s.auth). // todo remove with auth
		SetBody(input).
		SetResult(&GetFileURLOutput{}).
		SetError(&SyftAPIError{}).
		SetContext(ctx).
		Post(v1FilesDownload)

	if err != nil {
		return nil, err
	} else if res.IsError() {
		return nil, fmt.Errorf("error: %s", res.Error().(*SyftAPIError).Error)
	}

	return res.Result().(*GetFileURLOutput), nil
}

// ConnectWebsocket initiates a WebSocket connection
func (s *SyftAPI) ConnectWebsocket(ctx context.Context) error {
	return s.wsManager.Connect(ctx)
}

// SubscribeEvents returns a channel for receiving WebSocket events
// The subscription persists across reconnections
func (s *SyftAPI) SubscribeEvents() chan *message.Message {
	return s.wsManager.Subscribe()
}

// UnsubscribeEvents removes a subscription channel
func (s *SyftAPI) UnsubscribeEvents(ch chan *message.Message) {
	s.wsManager.Unsubscribe(ch)
}

func (s *SyftAPI) SendMessage(message *message.Message) error {
	return s.wsManager.SendMessage(message)
}

func (s *SyftAPI) WebsocketConnected() bool {
	return s.wsManager.IsConnected()
}
