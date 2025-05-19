package send

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type SendHandler struct {
	hub  *ws.WebsocketHub
	blob *blob.BlobService
}

func New(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	return &SendHandler{hub: hub, blob: blob}
}

func (h *SendHandler) SendMsg(ctx *gin.Context) {
	var header MessageHeaders
	if err := ctx.ShouldBindHeader(&header); err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	//  get the body
	body := ctx.Request.Body
	defer ctx.Request.Body.Close()

	// Read body Bytes
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// TODO: check if body bytes is not too large

	msg := syftmsg.NewHttpMsg(header.From, header.To, header.AppName, header.AppEp, header.Method, bodyBytes, header.Headers, header.Status, syftmsg.HttpMsgType(header.Type))

	slog.Info("sending message", "msg", msg)

	if header.Type == SendMsgResp {
		// TODO handler response messages
		return
	}

	//  send the message to the websocket hub
	if ok := h.hub.SendMessageUser(header.To, msg); !ok {
		// Handle saving to blob storage
		blobPath := fmt.Sprintf("http/%s/%s/%s", header.From, header.To, msg.Id)
		if _, err := h.blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
			Key:  blobPath,
			ETag: msg.Id,
			Body: bytes.NewReader(bodyBytes),
			Size: int64(len(bodyBytes)),
		}); err != nil {
			slog.Error("failed to save message to blob storage", "error", err)
			ctx.PureJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		slog.Info("saved message to blob storage", "blobPath", blobPath)
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"message":    "Request has been accepted",
			"request_id": msg.Data.(*syftmsg.HttpMsg).Id,
		})
		return
	}

	ctx.PureJSON(http.StatusOK, gin.H{"message": "Request accepted", "request_id": msg.Data.(*syftmsg.HttpMsg).Id})

}
