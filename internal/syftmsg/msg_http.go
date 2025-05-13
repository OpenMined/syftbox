package syftmsg


type HttpMessage struct {
	From string `json:"from"` // identifier of the sender
	To   string `json:"to"` // identifier of the receiver
	SyftURI string `json:"uri"` // syft uri
	AppName string `json:"app"` // identifier of the app
	AppEndpoint string `json:"appep"` // /endpoint
	Method string `json:"mthd"` // GET, POST, PUT, DELETE
	ContentType string `json:"ct"` // content type
	Body []byte `json:"body,omitempty"` // http body 
}

func NewHttpMessage(from string, to string, syftURI string, appName string, appEndpoint string, method string, body []byte) *Message {
	return &Message{
		Id :generateID(),
		Type: MsgHttp,
		Data: &HttpMessage{
			From: from,
			To: to,
			SyftURI: syftURI,
			AppName: appName,
			AppEndpoint: appEndpoint,
			Method: method,
			Body: body,
		},
	}
}