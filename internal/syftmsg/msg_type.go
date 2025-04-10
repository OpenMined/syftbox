package syftmsg

import "fmt"

type MessageType uint16

const (
	MsgSystem MessageType = iota
	MsgError
	MsgFileWrite
	MsgFileDelete
	MsgAck
	MsgNack
)

func (t MessageType) String() string {
	switch t {
	case MsgSystem:
		return "SYSTEM"
	case MsgError:
		return "ERROR"
	case MsgFileWrite:
		return "FILE_WRITE"
	case MsgFileDelete:
		return "FILE_DELETE"
	case MsgAck:
		return "ACK"
	case MsgNack:
		return "NACK"
	default:
		return fmt.Sprintf("???(%d)", t)
	}
}
