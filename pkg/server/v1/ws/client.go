package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/yashgorana/syftbox-go/pkg/utils"
)

const (
	pingPeriod     = 60 * time.Second
	writeTimeout   = 10 * time.Second
	shutdownReason = "shutdown"
)

// WsClient represents a connected WebSocket client.
type WsClient struct {
	id        string
	conn      *websocket.Conn
	send      chan interface{}
	Closed    chan struct{}
	wsDone    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewWsClient(conn *websocket.Conn) *WsClient {
	return &WsClient{
		id:     utils.TokenHex(3),
		conn:   conn,
		send:   make(chan interface{}, 256),
		Closed: make(chan struct{}),
		wsDone: make(chan struct{}),
	}
}

func (c *WsClient) Start(ctx context.Context) {
	slog.Debug("wsclient start", "id", c.id)
	c.wg.Add(2)
	go c.writeLoop(ctx)
	go c.readLoop(ctx)
}

func (c *WsClient) Close() {
	c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	c.wg.Wait() // wait for both read and write loops to finish
}

func (c *WsClient) closeConnection(status websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		// trigger internal close
		close(c.wsDone)
		c.conn.Close(status, reason)

		// wait for both read and write loops to finish
		c.wg.Wait()

		// trigger client close
		close(c.Closed)
		slog.Debug("wsclient closed", "id", c.id)
	})
}

func (c *WsClient) readLoop(ctx context.Context) {
	defer func() {
		slog.Debug("wsclient reader shutdown", "id", c.id)
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	}()

	for {
		mtype, msg, err := c.conn.Read(ctx)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
				// connection closed by client
			} else if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure && status != websocket.StatusNoStatusRcvd {
				slog.Warn("wsclient reader", "error", err, "id", c.id)
			}
			return
		}

		select {
		case <-c.wsDone:
			return

		case c.send <- msg:
			slog.Debug("wsclient reader", "mtype", mtype, "msg", msg, "id", c.id)

		default:
			slog.Warn("wsclient reader - send buffer full, disconnecting", "id", c.id)
			c.closeConnection(websocket.StatusPolicyViolation, "send buffer full")
			return
		}
	}
}

func (c *WsClient) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		slog.Debug("wsclient writer shutdown", "id", c.id)
		ticker.Stop()
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	}()

	for {
		select {
		case msg := <-c.send:
			ctxWrite, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(ctxWrite, c.conn, msg)
			cancel()
			if err != nil {
				slog.Error("wsclient writer", "error", err, "id", c.id)
				return
			}

		case <-ticker.C:
			ctxWrite, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(ctxWrite)
			cancel()
			if err != nil {
				slog.Error("wsclient writer ping", "error", err, "id", c.id)
				return
			}

		case <-c.wsDone:
			return

		case <-ctx.Done():
			return
		}
	}
}
