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
	MsgHttp
	MsgFileNotify
	MsgACLManifest
	MsgHotlinkOpen
	MsgHotlinkAccept
	MsgHotlinkReject
	MsgHotlinkData
	MsgHotlinkClose
	MsgHotlinkSignal
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
	case MsgHttp:
		return "HTTP"
	case MsgFileNotify:
		return "FILE_NOTIFY"
	case MsgACLManifest:
		return "ACL_MANIFEST"
	case MsgHotlinkOpen:
		return "HOTLINK_OPEN"
	case MsgHotlinkAccept:
		return "HOTLINK_ACCEPT"
	case MsgHotlinkReject:
		return "HOTLINK_REJECT"
	case MsgHotlinkData:
		return "HOTLINK_DATA"
	case MsgHotlinkClose:
		return "HOTLINK_CLOSE"
	case MsgHotlinkSignal:
		return "HOTLINK_SIGNAL"
	default:
		return fmt.Sprintf("???(%d)", t)
	}
}
