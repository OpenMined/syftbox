package syftmsg

import "github.com/google/uuid"

type HttpMsgType string

const (
	HttpMsgTypeRequest  HttpMsgType = "request"
	HttpMsgTypeResponse HttpMsgType = "response"
)

type HttpMsg struct {
	From    string            `json:"from"`
	To      string            `json:"to"`
	AppName string            `json:"app"`
	AppEp   string            `json:"appep"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
	Status  int               `json:"status,omitempty"`
	Id      string            `json:"id,omitempty"`
	Type    HttpMsgType       `json:"type,omitempty"`
}

func NewHttpMsg(
	from string,
	to string,
	appName string,
	appEp string,
	method string,
	body []byte,
	headers map[string]string,
	status int,
	msgType HttpMsgType,
) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHttp,
		Data: &HttpMsg{
			From:    from,
			To:      to,
			AppName: appName,
			AppEp:   appEp,
			Method:  method,
			Body:    body,
			Headers: headers,
			Status:  status,
			Id:      uuid.New().String(),
			Type:    msgType,
		},
	}
}
