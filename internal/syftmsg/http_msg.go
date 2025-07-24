package syftmsg

import (
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
	Id      string            `json:"id"`
	Etag    string            `json:"etag,omitempty"`
}

func NewHttpMsg(
	from string,
	syftURL utils.SyftBoxURL,
	method string,
	body []byte,
	headers map[string]string,
	id string,
	etag string,
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
			Id:      id,
			Etag:    etag,
		},
	}
}
