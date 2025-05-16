package syftmsg

type HttpMessage struct {
	From        string `json:"from"`             // identifier of the sender
	To          string `json:"to"`               // identifier of the receiver
	SyftURI     string `json:"uri"`              // syft uri
	AppName     string `json:"app"`              // identifier of the app
	AppEndpoint string `json:"appep"`            // /endpoint
	Method      string `json:"mthd"`             // GET, POST, PUT, DELETE
	ContentType string `json:"ct"`               // content type
	Body        []byte `json:"body,omitempty"`   // http body
	Status      string `json:"status,omitempty"` // http status
	RequestID   string `json:"request_id,omitempty"`
}

func NewHttpMessage(from string, to string, syftURI string, appName string, appEndpoint string, method string, contentType string, body []byte, status string, requestID string) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgHttp,
		Data: &HttpMessage{
			From:        from,
			To:          to,
			SyftURI:     syftURI,
			AppName:     appName,
			AppEndpoint: appEndpoint,
			Method:      method,
			ContentType: contentType,
			Body:        body,
			Status:      status,
			RequestID:   requestID,
		},
	}
}

// Http Message with string body
type HttpMessageWithStringBody struct {
	From        string `json:"from"`
	To          string `json:"to"`
	SyftURI     string `json:"uri"`
	AppName     string `json:"app"`
	AppEndpoint string `json:"appep"`
	Method      string `json:"mthd"`
	ContentType string `json:"ct"`
	Body        string `json:"body"`
	Status      string `json:"status"`
	RequestID   string `json:"request_id"`
}

func (m *HttpMessage) ToHttpMessageWithStringBody() *HttpMessageWithStringBody {
	return &HttpMessageWithStringBody{
		From:        m.From,
		To:          m.To,
		SyftURI:     m.SyftURI,
		AppName:     m.AppName,
		AppEndpoint: m.AppEndpoint,
		Method:      m.Method,
		ContentType: m.ContentType,
		Body:        string(m.Body),
		Status:      m.Status,
		RequestID:   m.RequestID,
	}
}
