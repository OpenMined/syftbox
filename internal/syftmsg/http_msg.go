package syftmsg

import (
	"github.com/google/uuid"
	"github.com/openmined/syftbox/internal/utils"
)

type HttpMsgType string

const (
	HttpMsgTypeRequest  HttpMsgType = "request"
	HttpMsgTypeResponse HttpMsgType = "response"
)

type HttpMsg struct {
	From    string            `json:"from"`
	SyftURL utils.SyftBoxURL  `json:"syft_url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
	Id      string            `json:"id,omitempty"`
	Type    HttpMsgType       `json:"type,omitempty"`
}

func NewHttpMsg(
	from string,
	syftURL utils.SyftBoxURL,
	method string,
	body []byte,
	headers map[string]string,
	msgType HttpMsgType,
) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHttp,
		Data: &HttpMsg{
			From:    from,
			SyftURL: syftURL,
			Method:  method,
			Body:    body,
			Headers: headers,
			Id:      uuid.New().String(),
			Type:    msgType,
		},
	}
}
