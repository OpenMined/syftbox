package syftmsg

type Error struct {
	Code    int    `json:"cod"`
	Path    string `json:"pth"`
	Message string `json:"msg"`
}

func NewError(code int, path string, msg string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgError,
		Data: &Error{
			Code:    code,
			Path:    path,
			Message: msg,
		},
	}
}
