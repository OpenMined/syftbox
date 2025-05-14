package syftsdk

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/openmined/syftbox/internal/syftmsg"
	"resty.dev/v3"
)

const (
	sendPath = "/api/v1/message/send"
)

type SendMsgAPI struct {
	client *resty.Client
}

func NewSendMsgAPI(client *resty.Client) *SendMsgAPI {
	return &SendMsgAPI{
		client: client,
	}
}

func (s *SendMsgAPI) Send(ctx context.Context, msg *syftmsg.HttpMessage, msgType string) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	var sdkError SyftSDKError

	res, err := s.client.R().
		SetContext(ctx).
		SetBody(msg.Body).
		SetHeaders(map[string]string{
			"Content-Type":      msg.ContentType,
			"x-syft-app":        msg.AppName,
			"x-syft-appep":      msg.AppEndpoint,
			"x-syft-mthd":       msg.Method,
			"x-syft-request-id": uuid.New().String(),
			"x-syft-msg-type":   msgType,
			"x-syft-from":       msg.From,
			"x-syft-to":         msg.To,
			"x-syft-uri":        msg.SyftURI,
			"x-syft-status":     msg.Status,
		}).
		SetResult(&resp).
		SetError(&sdkError).
		Post(sendPath)

	if err != nil {
		return nil, err
	} else if res.IsError() {
		return nil, fmt.Errorf("error: %s", sdkError.Error)
	}

	return &resp, nil
}
