package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/syftmsg"
)

const (
	maxMessageSize = 4 * 1024 * 1024 // 4MB
)

type WebsocketHub struct {
	clients  map[string]*WebsocketClient // map of ConnectionID -> Client
	register chan *WebsocketClient
	msgs     chan *ClientMessage

	wg sync.WaitGroup
	mu sync.RWMutex
}

func NewHub() *WebsocketHub {
	return &WebsocketHub{
		clients:  make(map[string]*WebsocketClient),
		register: make(chan *WebsocketClient),
		msgs:     make(chan *ClientMessage, 128),
	}
}

func (h *WebsocketHub) Run(ctx context.Context) {
	slog.Info("wshub started")
	defer slog.Info("wshub stopped")

	for {
		select {
		case client := <-h.register:

			h.mu.Lock()
			h.clients[client.ConnID] = client
			slog.Debug("wshub registered", "connId", client.ConnID, "user", client.Info.User, "active", len(h.clients))
			h.mu.Unlock()

			h.wg.Add(1)
			go client.Start(context.Background()) // todo should be ctx instead??
			go h.handleClientMessages(client)
			go func() {
				// if client closes, we just remove it from the hub
				<-client.Closed

				h.mu.Lock()
				defer h.mu.Unlock()

				delete(h.clients, client.ConnID)
				slog.Debug("wshub removed", "connId", client.ConnID, "user", client.Info.User, "active", len(h.clients))
				h.wg.Done()
			}()
		case <-ctx.Done():
			return
		}
	}
}

// OnMessage registers a handler function that gets called when a client sends a message
func (h *WebsocketHub) Messages() <-chan *ClientMessage {
	return h.msgs
}

func (h *WebsocketHub) Shutdown(ctx context.Context) {
	close(h.register)

	for _, client := range h.clients {
		go func() {
			// will automatically remove client from hub using the Closed channel
			client.Close()
			slog.Debug("wshub killed", "connId", client.ConnID)
		}()
	}

	h.wg.Wait()
	h.clients = nil
	slog.Info("wshub shutdown")
}

// WebsocketHandler is the handler for the websocket connection
// it upgrades the http connection to a websocket and registers the client with the hub
func (h *WebsocketHub) WebsocketHandler(ctx *gin.Context) {
	if ctx.GetString("user") == "" {
		ctx.Status(http.StatusUnauthorized)
		slog.Warn("wshub unauthorized", "ip", ctx.ClientIP(), "headers", ctx.Request.Header, "path", ctx.Request.URL)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := websocket.Accept(ctx.Writer, ctx.Request, nil)
	if err != nil {
		e := fmt.Errorf("websocket accept failed: %w", err)
		ctx.Error(e)
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": e.Error(),
		})
		return
	}
	conn.SetReadLimit(maxMessageSize)

	client := NewWebsocketClient(conn, &ClientInfo{
		User:    ctx.GetString("user"),
		IPAddr:  ctx.ClientIP(),
		Headers: ctx.Request.Header.Clone(),
	})

	client.MsgTx <- syftmsg.NewSystemMessage("0.5.0", "ok")

	h.register <- client
}

func (h *WebsocketHub) SendMessage(connId string, msg *syftmsg.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if client, ok := h.clients[connId]; ok {
		client.MsgTx <- msg
	}
}

// SendMessageUser sends a message to all clients with the specified username
func (h *WebsocketHub) SendMessageUser(user string, msg *syftmsg.Message) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sent := false

	for _, client := range h.clients {
		if client.Info.User == user {
			slog.Debug("sending message to client", "connId", client.ConnID, "user", user)
			select {
			case client.MsgTx <- msg:
				sent = true
			default:
				slog.Warn("wshub send buffer full", "connId", client.ConnID, "user", user)
			}
		}
	}

	return sent
}

func (h *WebsocketHub) Broadcast(msg *syftmsg.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.MsgTx <- msg:
		default:
			slog.Warn("wshub send buffer full", "connId", client.ConnID, "user", client.Info.User)
		}
	}
}

// BroadcastFiltered sends a message to all clients that match the filter
func (h *WebsocketHub) BroadcastFiltered(msg *syftmsg.Message, predicate func(*ClientInfo) bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if predicate(client.Info) {
			select {
			case client.MsgTx <- msg:
			default:
				slog.Warn("wshub send buffer full", "connId", client.ConnID, "user", client.Info.User)
			}
		}
	}
}

// handleClientMessages processes incoming messages from a client and calls registered handlers
func (h *WebsocketHub) handleClientMessages(client *WebsocketClient) {
	for {
		select {
		case <-client.Closed:
			return
		case msg, ok := <-client.MsgRx:
			if !ok {
				return
			}
			h.msgs <- &ClientMessage{
				ConnID:     client.ConnID,
				ClientInfo: client.Info,
				Message:    msg,
			}
		}
	}
}
