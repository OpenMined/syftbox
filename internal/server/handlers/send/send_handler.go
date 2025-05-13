package send

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type SendHandler struct {
	hub *ws.WebsocketHub
	blob *blob.BlobService
}

func NewSendHandler(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	return &SendHandler{
		hub: hub,
		blob: blob,
	}
}

func (h *SendHandler) HandleSendMessage(ctx *gin.Context) {
	// get the valid set of headers
	var header MessageHeader
	if err := ctx.ShouldBindHeader(&header); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	// get the message body
	body := ctx.Request.Body
	defer body.Close()

	// read the message body bytes

	// TODO: check if the body size is too large
	bodyBytes, err := io.ReadAll(body)

	if err != nil {
		ctx.PureJSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// request method
	method := ctx.Request.Method

	msg := syftmsg.NewHttpMessage(header.From, header.To, header.SyftURI, header.AppName, header.AppEndpoint, method, bodyBytes)

	slog.Debug("send message", "header", header, "body", string(bodyBytes), "method", method, "msg", msg)


	// send the message to the hub
	// TODO: check header. To is a valid email address
	if ok := h.hub.SendMessageUser(header.To, msg); !ok {
		// TODO: Receiver is not online
		// TODO: send the message to the blob storage
		// Return a 202 Accepted response
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"status": "message sent to blob storage",
			"blob":   "TODO: blob storage url",
		})
		return
	}

	ctx.PureJSON(http.StatusOK, gin.H{
		"status": "message sent",
	})
}