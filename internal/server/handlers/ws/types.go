package ws

import (
	"net/http"

	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/wsproto"
)

type ClientInfo struct {
	User    string
	IPAddr  string
	Headers http.Header
	Version string
	WSEncoding wsproto.Encoding
}

type ClientMessage struct {
	ConnID     string
	ClientInfo *ClientInfo
	Message    *syftmsg.Message
}
