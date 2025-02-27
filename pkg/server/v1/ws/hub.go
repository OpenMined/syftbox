package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
)

const (
	maxMessageSize = 1024 * 1024 // 1MB
)

type WebsocketHub struct {
	clients  map[string]*WsClient
	register chan *WsClient

	wg sync.WaitGroup
	mu sync.RWMutex
}

func NewHub() *WebsocketHub {
	return &WebsocketHub{
		clients:  make(map[string]*WsClient),
		register: make(chan *WsClient),
	}
}

func (h *WebsocketHub) Run(ctx context.Context) {
	slog.Info("wshub started")
	defer slog.Info("wshub stopped")

	for {
		select {
		case client := <-h.register:

			h.mu.Lock()
			h.clients[client.id] = client
			slog.Debug("wshub registered", "id", client.id, "connected", len(h.clients))
			h.mu.Unlock()

			h.wg.Add(1)
			go client.Start(context.Background())
			go func() {
				// if client closes, we just remove it from the hub
				<-client.Closed

				h.mu.Lock()
				defer h.mu.Unlock()

				delete(h.clients, client.id)
				slog.Debug("wshub removed", "id", client.id, "connected", len(h.clients))
				h.wg.Done()
			}()
		case <-ctx.Done():
			return
		}
	}
}

func (h *WebsocketHub) Shutdown(ctx context.Context) {
	close(h.register)

	for _, client := range h.clients {
		go func() {
			// will automatically remove client from hub using the Closed channel
			client.Close()
			slog.Debug("wshub killed", "id", client.id)
		}()
	}

	h.wg.Wait()
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

	client := NewWsClient(conn)

	wsjson.Write(ctx, conn, gin.H{
		"typ": "HELLO",
		"msg": "syftgo",
		"ver": "0.5.0",
		"cid": client.id,
	})

	h.register <- client
}

func (h *WebsocketHub) Broadcast(msg interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		client.send <- msg
	}
}
