package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/yashgorana/syftbox-go/internal/syftmsg"
	"github.com/yashgorana/syftbox-go/internal/utils"
)

const (
	writeTimeout   = 20 * time.Second
	shutdownReason = "shutdown"
)

type ClientInfo struct {
	User    string
	Headers http.Header
}

// WebsocketClient represents a connected WebSocket client.
type WebsocketClient struct {
	Id     string
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
		Id:     utils.TokenHex(3),
		Info:   info,
		MsgRx:  make(chan *syftmsg.Message, 8),
		MsgTx:  make(chan *syftmsg.Message, 8),
		Closed: make(chan struct{}),
		wsDone: make(chan struct{}),
		conn:   conn,
	}
}

func (c *WebsocketClient) Start(ctx context.Context) {
	slog.Debug("wsclient start", "id", c.Id)
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
		slog.Debug("wsclient closed", "id", c.Id)
	})
}

func (c *WebsocketClient) readLoop(ctx context.Context) {
	defer func() {
		slog.Debug("wsclient reader shutdown", "id", c.Id)
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, shutdownReason)
	}()

	var data *syftmsg.Message

	for {
		err := wsjson.Read(ctx, c.conn, &data)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				// connection closed by client
			} else if status := websocket.CloseStatus(err); status != websocket.StatusNormalClosure && status != websocket.StatusNoStatusRcvd {
				slog.Warn("wsclient reader", "error", err, "id", c.Id)
			}
			return
		}

		select {
		case <-c.wsDone:
			return

		case c.MsgRx <- data:
			// pushed to recieve queue

		default:
			slog.Warn("wsclient reader buffer full", "id", c.Id, "dropped", data)
		}
	}
}

func (c *WebsocketClient) writeLoop(ctx context.Context) {
	defer func() {
		slog.Debug("wsclient writer shutdown", "id", c.Id)
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
				slog.Error("wsclient writer", "error", err, "id", c.Id)
				return
			}

		case <-c.wsDone:
			return

		case <-ctx.Done():
			return
		}
	}
}
