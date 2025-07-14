package syftsdk

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
)

const (
	wsClientChannelSize  = 8
	wsClientPingPeriod   = 15 * time.Second
	wsClientPingTimeout  = 5 * time.Second
	wsClientWriteTimeout = 5 * time.Second
)

// wsClient represents a connected WebSocket client.
type wsClient struct {
	conn      *websocket.Conn       // websocket connection
	msgRx     chan *syftmsg.Message // messages received from the websocket
	msgTx     chan *syftmsg.Message // messages sent to the websocket
	closed    chan struct{}         // websocket is closed
	closing   chan struct{}         // websocket is closing
	closeOnce sync.Once             // closeOnce ensures the connection is closed only once
	wg        sync.WaitGroup        // waitGroup for the read and write loops
}

func newWSClient(conn *websocket.Conn) *wsClient {
	return &wsClient{
		msgRx:   make(chan *syftmsg.Message, wsClientChannelSize),
		msgTx:   make(chan *syftmsg.Message, wsClientChannelSize),
		closed:  make(chan struct{}),
		closing: make(chan struct{}),
		conn:    conn,
	}
}

func (c *wsClient) Start(ctx context.Context) {
	c.wg.Add(2)
	go c.writeLoop(ctx)
	go c.readLoop(ctx)
}

func (c *wsClient) Close() {
	c.closeConnection(websocket.StatusNormalClosure, "shutdown")
	// wait for both read and write loops to finish
	c.wg.Wait()
}

func (c *wsClient) closeConnection(status websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		// trigger internal close
		close(c.closing)
		c.conn.Close(status, reason)

		// wait for both read and write loops to finish
		c.wg.Wait()

		// trigger client close
		close(c.closed)
		close(c.msgRx)
		close(c.msgTx)
	})
}

func (c *wsClient) readLoop(ctx context.Context) {
	defer func() {
		slog.Debug("socket reader shutdown")
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, "shutdown")
	}()

	var data *syftmsg.Message

	for {
		select {
		case <-ctx.Done():
			return

		default:
			// Continue with read attempt
		}

		err := wsjson.Read(ctx, c.conn, &data)
		if err != nil {
			if !isWSExpectedCloseError(err) {
				slog.Warn("socket RECV", "error", err)
			}
			return
		}
		slog.Debug("socket RECV", "id", data.Id, "type", data.Type)

		select {
		case <-c.closing:
			return

		case c.msgRx <- data:
			// do nothing

		default:
			slog.Warn("socket RECV buffer full", "dropped", data)
		}
	}
}

func (c *wsClient) writeLoop(ctx context.Context) {
	pingTicker := time.NewTicker(wsClientPingPeriod)
	defer func() {
		slog.Debug("socket writer shutdown")
		pingTicker.Stop()
		c.wg.Done()
		c.closeConnection(websocket.StatusNormalClosure, "shutdown")
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case <-c.closing:
			return

		case msg, ok := <-c.msgTx:
			if !ok {
				return
			}

			slog.Debug("socket SEND", "id", msg.Id, "type", msg.Type)

			// write message within timeout
			ctxWrite, cancel := context.WithTimeout(ctx, wsClientWriteTimeout)
			err := wsjson.Write(ctxWrite, c.conn, msg)
			cancel()

			if err != nil {
				slog.Error("socket SEND", "error", err)
				return
			}

		case <-pingTicker.C:
			// Send ping to keep connection alive
			ctxPing, cancel := context.WithTimeout(ctx, wsClientPingTimeout)
			err := c.conn.Ping(ctxPing)
			cancel()

			if err != nil {
				slog.Error("socket PING", "error", err)
				return
			}
		}
	}
}

// isWSExpectedCloseError returns true if the error is an expected connection closure
func isWSExpectedCloseError(err error) bool {
	// Check for normal close scenarios
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
		return true
	}

	// Check for common network errors
	return errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed)
}
