package send

type MsgType string

const (
	SendMsgReq  MsgType = "request"
	SendMsgResp MsgType = "response"
)

type MessageHeaders struct {
	Type    MsgType           `header:"x-syft-msg-type" binding:"required"`
	From    string            `header:"x-syft-from" binding:"required"`
	To      string            `header:"x-syft-to" binding:"required"`
	AppName string            `header:"x-syft-app" binding:"required"`
	AppEp   string            `header:"x-syft-appep" binding:"required"`
	Method  string            `header:"x-syft-method" binding:"required"`
	Headers map[string]string `header:"x-syft-headers"`
	Status  int               `header:"x-syft-status"`
}
