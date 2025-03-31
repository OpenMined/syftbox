package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/message"
)

const (
	maxMessageSize = 4 * 1024 * 1024 // 1MB
)

type ClientMessage struct {
	ClientId string
	Info     *ClientInfo
	Message  *message.Message
}

type WebsocketHub struct {
	clients  map[string]*WebsocketClient
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
			h.clients[client.Id] = client
			slog.Debug("wshub registered", "id", client.Id, "total", len(h.clients))
			h.mu.Unlock()

			h.wg.Add(1)
			go client.Start(context.Background())
			go h.handleClientMessages(client)
			go func() {
				// if client closes, we just remove it from the hub
				<-client.Closed

				h.mu.Lock()
				defer h.mu.Unlock()

				delete(h.clients, client.Id)
				slog.Debug("wshub removed", "id", client.Id, "total", len(h.clients))
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
			slog.Debug("wshub killed", "id", client.Id)
		}()
	}

	h.wg.Wait()
	h.clients = nil
	slog.Info("wshub shutdown")
}

func (h *WebsocketHub) WebsocketHandler(ctx *gin.Context) {
	// Upgrade HTTP connection to WebSocket
	conn, err := websocket.Accept(ctx.Writer, ctx.Request, nil)
	if err != nil {
		ctx.PureJSON(http.StatusBadRequest, gin.H{
			"error": "websocket accept failed: " + err.Error(),
		})
		return
	}
	conn.SetReadLimit(maxMessageSize)

	client := NewWebsocketClient(conn, &ClientInfo{
		User:    ctx.GetString("user"),
		Headers: ctx.Request.Header.Clone(),
	})

	client.MsgTx <- message.NewSystemMessage("0.5.0", "ok")

	h.register <- client
}

func (h *WebsocketHub) SendMessage(clientId string, msg *message.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if client, ok := h.clients[clientId]; ok {
		client.MsgTx <- msg
	}
}

// SendMessageUser sends a message to all clients with the specified username
func (h *WebsocketHub) SendMessageUser(user string, msg *message.Message) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sent := false

	for _, client := range h.clients {
		if client.Info.User == user {
			select {
			case client.MsgTx <- msg:
				sent = true
			default:
				slog.Warn("wshub send buffer full", "id", client.Id, "user", user)
			}
		}
	}

	return sent
}

func (h *WebsocketHub) Broadcast(msg *message.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		client.MsgTx <- msg
	}
}

// BroadcastFiltered sends a message to all clients that match the filter
func (h *WebsocketHub) BroadcastFiltered(msg *message.Message, predicate func(*ClientInfo) bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if predicate(client.Info) {
			client.MsgTx <- msg
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
				ClientId: client.Id,
				Info:     client.Info,
				Message:  msg,
			}
		}
	}
}
