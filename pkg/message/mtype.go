package message

import "fmt"

type MessageType uint16

func (t MessageType) String() string {
	switch t {
	case MsgSystem:
		return "SYSTEM"
	case MsgError:
		return "ERROR"
	case MsgFileWrite:
		return "FILE_WRITE"
	case MsgFileMove:
		return "FILE_MOVE"
	case MsgFileDelete:
		return "FILE_DELETE"
	default:
		return fmt.Sprintf("???(%d)", t)
	}
}
