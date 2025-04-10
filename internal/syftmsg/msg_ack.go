package syftmsg

type Ack struct{}

func NewAck(id string) *Message {
	return &Message{
		Id:   id,
		Type: MsgAck,
		Data: &Ack{},
	}
}

type Nack struct {
	Error string `json:"err"`
}

func NewNack(id string, err string) *Message {
	return &Message{
		Id:   id,
		Type: MsgNack,
		Data: &Nack{Error: err},
	}
}
