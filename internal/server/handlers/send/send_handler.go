package send

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
)

type SendHandler struct {
	hub              *ws.WebsocketHub
	blob             *blob.BlobService
	responseChannels map[string]chan *syftmsg.Message
	mu               sync.RWMutex
}

func NewSendHandler(hub *ws.WebsocketHub, blob *blob.BlobService) *SendHandler {
	return &SendHandler{
		hub:              hub,
		blob:             blob,
		responseChannels: make(map[string]chan *syftmsg.Message),
	}
}

func generateChannelKey(user string, appName string, endpoint string) string {
	return fmt.Sprintf("%s:%s:%s", user, appName, endpoint)
}

func (h *SendHandler) getOrCreateChannel(key string) chan *syftmsg.Message {
	defer h.mu.RUnlock()
	h.mu.RLock()
	ch, ok := h.responseChannels[key]
	if !ok {
		ch = make(chan *syftmsg.Message)
		h.responseChannels[key] = ch
	}
	return ch
}

func (h *SendHandler) getChannel(key string) (chan *syftmsg.Message, bool) {
	defer h.mu.RUnlock()
	h.mu.RLock()
	ch, ok := h.responseChannels[key]
	return ch, ok
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

	msg := syftmsg.NewHttpMessage(header.From, header.To, header.SyftURI, header.AppName, header.AppEndpoint, method, header.ContentType, bodyBytes, header.Status)

	slog.Info("send message", "header", header, "body", string(bodyBytes), "method", method, "msg", msg)

	if header.Type == SendMessageResponse {
		key := generateChannelKey(header.To, header.AppName, header.AppEndpoint)
		ch := h.getOrCreateChannel(key)
		go func() {
			ch <- msg
			slog.Info("add message to channel", "key", key, "msg", msg)
		}()
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"status": fmt.Sprintf("message sent to channel %s", key),
		})
		return
	}

	// send the message to the hub
	// TODO: check header. To is a valid email address
	if ok := h.hub.SendMessageUser(header.To, msg); !ok {
		// TODO: Receiver is not online
		// TODO: send the message to the blob storage
		// Return a 202 Accepted response
		ctx.PureJSON(http.StatusAccepted, gin.H{
			"status": "message sent to blob storage",
		})
		return
	}

	ctx.PureJSON(http.StatusOK, gin.H{
		"status": "message sent",
	})
}

func (h *SendHandler) HandleGetMessage(ctx *gin.Context) {
	user := ctx.Query("user_id")
	appName := ctx.Query("app")
	endpoint := ctx.Query("endpoint")

	key := generateChannelKey(user, appName, endpoint)

	ch, ok := h.getChannel(key)
	if !ok {
		ctx.PureJSON(http.StatusNotFound, gin.H{"error": "No request sent to this endpoint"})
		return
	}

	select {
	case msg := <-ch:
		ctx.JSON(http.StatusOK, msg.Data)
		return
	case <-time.After(1 * time.Second):
		ctx.PureJSON(http.StatusNotFound, gin.H{"error": "No new messages"})
		return
	}
}
