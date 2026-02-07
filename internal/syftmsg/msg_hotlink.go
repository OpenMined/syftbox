package syftmsg

type HotlinkOpen struct {
	SessionID string `json:"sid" msgpack:"sid"`
	Path      string `json:"pth" msgpack:"pth"`
}

type HotlinkAccept struct {
	SessionID string `json:"sid" msgpack:"sid"`
}

type HotlinkReject struct {
	SessionID string `json:"sid" msgpack:"sid"`
	Reason    string `json:"rsn,omitempty" msgpack:"rsn,omitempty"`
}

type HotlinkData struct {
	SessionID string `json:"sid" msgpack:"sid"`
	Seq       uint64 `json:"seq" msgpack:"seq"`
	Path      string `json:"pth" msgpack:"pth"`
	ETag      string `json:"etg,omitempty" msgpack:"etg,omitempty"`
	Payload   []byte `json:"pay,omitempty" msgpack:"pay,omitempty"`
}

type HotlinkClose struct {
	SessionID string `json:"sid" msgpack:"sid"`
	Reason    string `json:"rsn,omitempty" msgpack:"rsn,omitempty"`
}

type HotlinkSignal struct {
	SessionID string   `json:"sid" msgpack:"sid"`
	Kind      string   `json:"knd" msgpack:"knd"`
	Addrs     []string `json:"adr,omitempty" msgpack:"adr,omitempty"`
	Token     string   `json:"tok,omitempty" msgpack:"tok,omitempty"`
	Error     string   `json:"err,omitempty" msgpack:"err,omitempty"`
}

func NewHotlinkOpen(sessionID, path string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkOpen,
		Data: &HotlinkOpen{
			SessionID: sessionID,
			Path:      path,
		},
	}
}

func NewHotlinkAccept(sessionID string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkAccept,
		Data: &HotlinkAccept{
			SessionID: sessionID,
		},
	}
}

func NewHotlinkReject(sessionID, reason string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkReject,
		Data: &HotlinkReject{
			SessionID: sessionID,
			Reason:    reason,
		},
	}
}

func NewHotlinkData(sessionID string, seq uint64, path string, etag string, payload []byte) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkData,
		Data: &HotlinkData{
			SessionID: sessionID,
			Seq:       seq,
			Path:      path,
			ETag:      etag,
			Payload:   payload,
		},
	}
}

func NewHotlinkClose(sessionID, reason string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkClose,
		Data: &HotlinkClose{
			SessionID: sessionID,
			Reason:    reason,
		},
	}
}

func NewHotlinkSignal(sessionID, kind string, addrs []string, token string, err string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHotlinkSignal,
		Data: &HotlinkSignal{
			SessionID: sessionID,
			Kind:      kind,
			Addrs:     addrs,
			Token:     token,
			Error:     err,
		},
	}
}
