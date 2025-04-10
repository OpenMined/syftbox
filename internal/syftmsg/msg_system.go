package syftmsg

type System struct {
	SystemVersion string `json:"ver"`
	Message       string `json:"msg"`
}

func NewSystemMessage(version string, msg string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgSystem,
		Data: &System{
			SystemVersion: version,
			Message:       msg,
		},
	}
}
