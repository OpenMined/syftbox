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
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/wsproto"
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
		typ, raw, err := c.conn.Read(ctx)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				// connection closed by client
			} else if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure && status != websocket.StatusNoStatusRcvd {
				slog.Warn("wsclient reader", "error", err, "connId", c.ConnID)
			}
			return
		}

		msg, _, uerr := wsproto.Unmarshal(typ, raw)
		if uerr != nil {
			slog.Warn("wsclient reader decode", "error", uerr, "connId", c.ConnID)
			continue
		}

		select {
		case <-c.wsDone:
			return

		case c.MsgRx <- msg:
			// pushed to recieve queue

		default:
			slog.Warn("wsclient reader buffer full", "connId", c.ConnID)
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
			typ, payload, err := wsproto.Marshal(msg, c.Info.WSEncoding)
			if err == nil {
				err = c.conn.Write(ctxWrite, typ, payload)
			}
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
