package send

type MessageType string

const (
	SendMessageRequest  MessageType = "request"
	SendMessageResponse MessageType = "response"
)

type MessageHeader struct {
	Type        MessageType `header:"x-syft-msg-type" binding:"required"`
	From        string      `header:"x-syft-from" binding:"required"`
	To          string      `header:"x-syft-to" binding:"required"`
	AppName     string      `header:"x-syft-app" binding:"required"`
	AppEndpoint string      `header:"x-syft-app-endpoint" binding:"required"`
	SyftURI     string      `header:"x-syft-syft-uri" binding:"required"`
	ContentType string      `header:"Content-Type" binding:"required"`
	RequestID   string      `header:"x-syft-request-id"`
}
