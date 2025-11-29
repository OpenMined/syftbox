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
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
)

const (
	writeTimeout   = 20 * time.Second
	shutdownReason = "shutdown"
)

// WebsocketClient represents a connected WebSocket client.
type WebsocketClient struct {
	ConnID string
	Info   *ClientInfo
	MsgRx  chan *syftmsg.Message
	MsgTx  chan *syftmsg.Message
	Closed chan struct{}

	conn      *websocket.Conn
	wsDone    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewWebsocketClient(conn *websocket.Conn, info *ClientInfo) *WebsocketClient {
	return &WebsocketClient{
		ConnID: utils.TokenHex(4),
		Info:   info,
		MsgRx:  make(chan *syftmsg.Message, 256), // Increased from 8 to handle burst traffic
		MsgTx:  make(chan *syftmsg.Message, 256), // Increased from 8 to handle burst traffic
		Closed: make(chan struct{}),
		wsDone: make(chan struct{}),
		conn:   conn,
	}
}

func (c *WebsocketClient) Start(ctx context.Context) {
	slog.Debug("wsclient start", "connId", c.ConnID)
	c.wg.Add(2)
	go c.writeLoop(ctx)
	go c.readLoop(ctx)
}

func (c *WebsocketClient) Close() {
	c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	// wait for both read and write loops to finish
	c.wg.Wait()
}

func (c *WebsocketClient) closeConnection(status websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		// trigger internal close
		close(c.wsDone)
		c.conn.Close(status, reason)

		// wait for both read and write loops to finish
		c.wg.Wait()

		// trigger client close
		close(c.Closed)
		close(c.MsgRx)
		close(c.MsgTx)
		slog.Debug("wsclient closed", "connId", c.ConnID)
	})
}

func (c *WebsocketClient) readLoop(ctx context.Context) {
	defer func() {
		slog.Debug("wsclient reader shutdown", "connId", c.ConnID)
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	}()

	for {
		var data *syftmsg.Message

		err := wsjson.Read(ctx, c.conn, &data)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				// connection closed by client
			} else if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure && status != websocket.StatusNoStatusRcvd {
				slog.Warn("wsclient reader", "error", err, "connId", c.ConnID)
			}
			return
		}

		select {
		case <-c.wsDone:
			return

		case c.MsgRx <- data:
			// pushed to recieve queue

		default:
			slog.Warn("wsclient reader buffer full", "connId", c.ConnID, "dropped", data)
		}
	}
}

func (c *WebsocketClient) writeLoop(ctx context.Context) {
	defer func() {
		slog.Debug("wsclient writer shutdown", "connId", c.ConnID)
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	}()

	for {
		select {
		case msg := <-c.MsgTx:

			// write message within timeout
			ctxWrite, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(ctxWrite, c.conn, msg)
			cancel()
			if err != nil {
				slog.Error("wsclient writer", "connId", c.ConnID, "msgId", msg.Id, "msgType", msg.Type, "error", err)
			} else {
				slog.Debug("wsclient writer", "connId", c.ConnID, "msgId", msg.Id, "msgType", msg.Type)
			}

		case <-c.wsDone:
			return

		case <-ctx.Done():
			return
		}
	}
}
