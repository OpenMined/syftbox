package message

import "github.com/yashgorana/syftbox-go/internal/utils"

const IdSize = 3

func NewFileDelete(path string) *Message {
	return &Message{
		Id:   utils.TokenHex(IdSize),
		Type: MsgFileDelete,
		Data: &FileDelete{
			Path: path,
		},
	}
}

func NewFileWrite(path string, etag string, length int64, contents []byte) *Message {
	return &Message{
		Id:   utils.TokenHex(IdSize),
		Type: MsgFileWrite,
		Data: &FileWrite{
			Path:    path,
			Etag:    etag,
			Length:  length,
			Content: contents,
		},
	}
}

func NewSystemMessage(version string, msg string) *Message {
	return &Message{
		Id:   utils.TokenHex(IdSize),
		Type: MsgSystem,
		Data: &System{
			SystemVersion: version,
			Message:       msg,
		},
	}
}

func NewError(code int, path string, msg string) *Message {
	return &Message{
		Id:   utils.TokenHex(IdSize),
		Type: MsgError,
		Data: &Error{
			Code:    code,
			Path:    path,
			Message: msg,
		},
	}
}
