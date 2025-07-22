package send

import (
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type WSMsgDispatcher struct {
	hub *ws.WebsocketHub
}

func (m *WSMsgDispatcher) Dispatch(datasite string, msg *syftmsg.Message) bool {
	return m.hub.SendMessageUser(datasite, msg)
}

func NewWSMsgDispatcher(hub *ws.WebsocketHub) MessageDispatcher {
	return &WSMsgDispatcher{hub: hub}
}

var _ RPCMsgStore = &BlobMsgStore{}
