package syftmsg

type FileWrite struct {
	Path    string `json:"pth"`
	ETag    string `json:"etg"`
	Length  int64  `json:"len"`
	Content []byte `json:"con,omitempty"`
}

type FileDelete struct {
	Path string `json:"pth"`
}

func NewFileWrite(path string, etag string, length int64, contents []byte) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgFileWrite,
		Data: &FileWrite{
			Path:    path,
			ETag:    etag,
			Length:  length,
			Content: contents,
		},
	}
}

func NewFileDelete(path string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgFileDelete,
		Data: &FileDelete{
			Path: path,
		},
	}
}
