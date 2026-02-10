package syftmsg

type Ack struct {
	OriginalId string `json:"oid"`
}

func NewAck(originalMsgId string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgAck,
		Data: &Ack{OriginalId: originalMsgId},
	}
}

type Nack struct {
	OriginalId string `json:"oid"`
	Error      string `json:"err"`
}

func NewNack(originalMsgId string, err string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgNack,
		Data: &Nack{
			OriginalId: originalMsgId,
			Error:      err,
		},
	}
}
