package ws

import (
	"net/http"

	"github.com/openmined/syftbox/internal/syftmsg"
)

type ClientInfo struct {
	User    string
	IPAddr  string
	Headers http.Header
	Version string
}

type ClientMessage struct {
	ConnID     string
	ClientInfo *ClientInfo
	Message    *syftmsg.Message
}
