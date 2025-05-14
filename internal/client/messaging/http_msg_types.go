package messaging

import "github.com/openmined/syftbox/internal/syftmsg"

type HttpResponseMsg struct {
	Message *syftmsg.HttpMessage
	Error   error
}

type HttpRequestMsg struct {
	Message *syftmsg.HttpMessage
}
