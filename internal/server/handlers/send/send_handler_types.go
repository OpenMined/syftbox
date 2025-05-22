package send

type MsgType string

const (
	SendMsgReq  MsgType = "request"
	SendMsgResp MsgType = "response"
)

type MessageRequest struct {
	Type    MsgType           `header:"x-syft-msg-type" binding:"required"`
	From    string            `header:"x-syft-from" binding:"required"`
	To      string            `header:"x-syft-to" binding:"required"`
	AppName string            `header:"x-syft-app" binding:"required"`
	AppEp   string            `header:"x-syft-appep" binding:"required"`
	Method  string            `header:"x-syft-method" binding:"required"`
	Headers map[string]string `header:"x-syft-headers"`
	Status  int               `header:"x-syft-status"`
	Timeout int               `form:"timeout" header:"x-syft-timeout" binding:"gte=0"`
}

type PollForObjectQuery struct {
	RequestID string `form:"request_id" binding:"required"`
	AppName   string `form:"app_name" binding:"required"`
	AppEp     string `form:"app_endpoint" binding:"required"`
	User      string `form:"user" binding:"required"`
	Timeout   int    `form:"timeout,omitempty" binding:"gte=0"`
}
