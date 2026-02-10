package syftsdk

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/openmined/syftbox/internal/wsproto"
)

const (
	eventsBufferSize        = 256 // Increased from 16 to handle burst traffic (100+ files)
	eventsReconnectDelay    = 1 * time.Second
	eventsMaxReconnectDelay = 8 * time.Second
	eventsReconnectTimeout  = 10 * time.Second
	wsClientMaxMessageSize  = 8 * 1024 * 1024 // 8MB (to handle 4MB files + JSON overhead)
	eventsPath              = "/api/v1/events"
	sendMaxRetries          = 5
	sendInitialBackoff      = 10 * time.Millisecond
	sendMaxBackoff          = 500 * time.Millisecond
)

// pendingAck represents a pending acknowledgment
type pendingAck struct {
	ackChan chan *syftmsg.Message
	timeout *time.Timer
}

// EventsAPI manages real-time event communication
type EventsAPI struct {
	client           *req.Client
	wsClient         *wsClient
	messages         chan *syftmsg.Message
	overflowQueue    chan *syftmsg.Message
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.RWMutex
	connected        bool
	reconnectAttempt int
	pendingAcks      map[string]*pendingAck
	ackMu            sync.RWMutex
	stats            *wsStats
}

// newEventsAPI creates a new EventsAPI instance
func newEventsAPI(client *req.Client) *EventsAPI {
	ctx, cancel := context.WithCancel(context.Background())

	api := &EventsAPI{
		client:        client,
		ctx:           ctx,
		cancel:        cancel,
		messages:      make(chan *syftmsg.Message, eventsBufferSize),
		overflowQueue: make(chan *syftmsg.Message, eventsBufferSize*2), // 2x buffer for overflow
		pendingAcks:   make(map[string]*pendingAck),
		stats:         newWSStats(),
	}

	// Start overflow queue processor
	go api.processOverflowQueue()

	return api
}

// Connect initiates a WebSocket connection
func (e *EventsAPI) Connect(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.connected && e.wsClient != nil {
		return nil
	}

	wsClient, err := e.connectLocked(ctx)
	if err != nil {
		return fmt.Errorf("sdk: events: connect failed: %w", err)
	}

	go e.manageConnection(wsClient)
	return nil
}

// IsConnected returns the current connection status
func (e *EventsAPI) IsConnected() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.connected
}

// Get returns a channel for receiving WebSocket messages
func (e *EventsAPI) Get() <-chan *syftmsg.Message {
	return e.messages
}

// Send sends a message through the WebSocket with retry on queue full
// If retries fail, message is queued in overflow buffer for later delivery
func (e *EventsAPI) Send(msg *syftmsg.Message) error {
	// Try direct send first
	if err := e.sendDirect(msg); err == nil {
		return nil
	}

	// Direct send failed, try overflow queue
	select {
	case e.overflowQueue <- msg:
		slog.Info("socketmgr SEND overflow queued", "id", msg.Id, "type", msg.Type, "queueLen", len(e.overflowQueue))
		return nil
	default:
		slog.Error("socketmgr SEND overflow queue full", "id", msg.Id, "type", msg.Type)
		return ErrEventsMessageQueueFull
	}
}

// SendWithAck sends a message and waits for ACK/NACK response
// Returns error if NACK received or timeout occurs
func (e *EventsAPI) SendWithAck(msg *syftmsg.Message, timeout time.Duration) error {
	// Register pending ACK before sending
	ackChan := make(chan *syftmsg.Message, 1)
	timer := time.NewTimer(timeout)

	pending := &pendingAck{
		ackChan: ackChan,
		timeout: timer,
	}

	e.ackMu.Lock()
	e.pendingAcks[msg.Id] = pending
	e.ackMu.Unlock()

	// Cleanup on exit
	defer func() {
		timer.Stop()
		e.ackMu.Lock()
		delete(e.pendingAcks, msg.Id)
		e.ackMu.Unlock()
	}()

	// Send the message
	if err := e.Send(msg); err != nil {
		return err
	}

	// Wait for ACK/NACK or timeout
	select {
	case ackMsg := <-ackChan:
		switch ackMsg.Type {
		case syftmsg.MsgAck:
			slog.Debug("socketmgr ACK received", "id", msg.Id)
			return nil
		case syftmsg.MsgNack:
			nackData, ok := ackMsg.Data.(syftmsg.Nack)
			if ok {
				return fmt.Errorf("NACK received: %s", nackData.Error)
			}
			return fmt.Errorf("NACK received")
		default:
			return fmt.Errorf("unexpected response type: %s", ackMsg.Type)
		}
	case <-timer.C:
		return fmt.Errorf("ACK timeout after %v", timeout)
	case <-e.ctx.Done():
		return fmt.Errorf("context cancelled")
	}
}

// Close terminates the WebSocket connection and cleans up
func (e *EventsAPI) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.cancel()

	if e.wsClient != nil {
		e.wsClient.Close()
		e.wsClient = nil
	}

	e.connected = false
	slog.Info("socketmgr closed")
}

// Stats returns a point-in-time snapshot of websocket telemetry.
func (e *EventsAPI) Stats() EventsStatsSnapshot {
	e.mu.RLock()
	wsClient := e.wsClient
	connected := e.connected
	reconnectAttempt := e.reconnectAttempt
	overflowLen := len(e.overflowQueue)
	e.mu.RUnlock()

	txLen, rxLen := 0, 0
	enc := ""
	if wsClient != nil {
		txLen = len(wsClient.msgTx)
		rxLen = len(wsClient.msgRx)
		enc = wsClient.encoding.String()
	}

	lastErr, _ := e.stats.lastErrorValue.Load().(string)

	return EventsStatsSnapshot{
		Connected:        connected,
		Encoding:         enc,
		ReconnectAttempt: reconnectAttempt,
		Reconnects:       e.stats.reconnects.Load(),
		TxQueueLen:       txLen,
		RxQueueLen:       rxLen,
		OverflowQueueLen: overflowLen,
		BytesSentTotal:   e.stats.bytesSent.Load(),
		BytesRecvTotal:   e.stats.bytesRecv.Load(),
		ConnectedAtNs:    e.stats.connectedAtNs.Load(),
		DisconnectedAtNs: e.stats.disconnAtNs.Load(),
		LastSentAtNs:     e.stats.lastSentNs.Load(),
		LastRecvAtNs:     e.stats.lastRecvNs.Load(),
		LastPingAtNs:     e.stats.lastPingNs.Load(),
		LastError:        lastErr,
	}
}

// processOverflowQueue drains the overflow queue and sends messages when capacity is available
func (e *EventsAPI) processOverflowQueue() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case msg := <-e.overflowQueue:
			// Try to send with retries
			if err := e.sendDirect(msg); err != nil {
				slog.Warn("socketmgr overflow queue failed to send", "id", msg.Id, "error", err)
			}
		}
	}
}

// sendDirect sends a message directly without using overflow queue
func (e *EventsAPI) sendDirect(msg *syftmsg.Message) error {
	e.mu.RLock()
	wsClient := e.wsClient
	connected := e.connected
	e.mu.RUnlock()

	if !connected || wsClient == nil {
		return ErrEventsNotConnected
	}

	backoff := sendInitialBackoff
	for attempt := 0; attempt < sendMaxRetries; attempt++ {
		select {
		case wsClient.msgTx <- msg:
			if attempt > 0 {
				slog.Debug("socketmgr SEND", "id", msg.Id, "type", msg.Type, "retries", attempt)
			} else {
				slog.Debug("socketmgr SEND", "id", msg.Id, "type", msg.Type)
			}
			return nil
		default:
			if attempt < sendMaxRetries-1 {
				slog.Debug("socketmgr SEND queue full, retrying", "id", msg.Id, "attempt", attempt+1, "backoff", backoff)
				time.Sleep(backoff)
				backoff = time.Duration(float64(backoff) * 2)
				if backoff > sendMaxBackoff {
					backoff = sendMaxBackoff
				}
			}
		}
	}

	return ErrEventsMessageQueueFull
}

// connectLocked creates a new WebSocket connection (must be called with lock held)
func (e *EventsAPI) connectLocked(ctx context.Context) (*wsClient, error) {
	// Clean up any existing connection
	if e.wsClient != nil {
		e.wsClient.Close()
		e.wsClient = nil
		e.connected = false
	}

	// Build URL with auth query parameters
	url, err := e.fullURL()
	if err != nil {
		return nil, fmt.Errorf("sdk: events: failed to get full url: %w", err)
	}

	// Connect with auth in headers
	headers := e.client.Headers.Clone()
	headers.Set("X-Syft-WS-Encodings", "msgpack,json")
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: headers, // this will include the auth token
	})
	if err != nil {
		return nil, fmt.Errorf("sdk: events: failed to connect to %s: %w", url, err)
	}
	conn.SetReadLimit(wsClientMaxMessageSize)

	encHeader := ""
	if resp != nil {
		encHeader = resp.Header.Get("X-Syft-WS-Encoding")
	}
	enc := wsproto.PreferredEncoding(encHeader)

	// Create and start client
	wsClient := newWSClient(conn, enc, e.stats)
	wsClient.Start(e.ctx)

	e.wsClient = wsClient
	e.connected = true
	e.stats.onConnected()

	slog.Info("socketmgr client connected")
	return wsClient, nil
}

// manageConnection handles the WebSocket connection lifecycle
func (e *EventsAPI) manageConnection(wsClient *wsClient) {
	go e.consumeMessages(wsClient)

	select {
	case <-wsClient.closed:
		slog.Info("socketmgr client disconnected, will reconnect")

		e.mu.Lock()
		if e.wsClient == wsClient {
			e.wsClient = nil
			e.connected = false
			e.reconnectAttempt = 0
			e.stats.onDisconnected()
		}
		e.mu.Unlock()

		select {
		case <-e.ctx.Done():
			return
		default:
			e.reconnectWithBackoff()
		}

	case <-e.ctx.Done():
		return
	}
}

// consumeMessages processes incoming messages from the websocket client
func (e *EventsAPI) consumeMessages(wsClient *wsClient) {
	for {
		select {
		case <-e.ctx.Done():
			return

		case <-wsClient.closed:
			return

		case msg, ok := <-wsClient.msgRx:
			if !ok {
				slog.Debug("socketmgr RECV closed")
				return
			}

			slog.Debug("socketmgr RECV", "id", msg.Id, "type", msg.Type)

			// Handle ACK/NACK messages specially
			if msg.Type == syftmsg.MsgAck || msg.Type == syftmsg.MsgNack {
				var originalId string

				if msg.Type == syftmsg.MsgAck {
					if ackData, ok := msg.Data.(syftmsg.Ack); ok {
						originalId = ackData.OriginalId
					}
				} else if msg.Type == syftmsg.MsgNack {
					if nackData, ok := msg.Data.(syftmsg.Nack); ok {
						originalId = nackData.OriginalId
					}
				}

				if originalId != "" {
					e.ackMu.RLock()
					pending, exists := e.pendingAcks[originalId]
					e.ackMu.RUnlock()

					if exists {
						select {
						case pending.ackChan <- msg:
							slog.Debug("socketmgr ACK/NACK delivered", "originalId", originalId, "type", msg.Type)
						default:
							slog.Warn("socketmgr ACK/NACK channel full", "originalId", originalId)
						}
						continue
					}
				}

				slog.Debug("socketmgr ACK/NACK no pending request", "id", msg.Id, "type", msg.Type)
				continue
			}

			// Forward non-ACK/NACK messages to the main channel
			select {
			case e.messages <- msg:
				// Successfully delivered
			default:
				slog.Warn("socketmgr RECV buffer full. dropped", "id", msg.Id, "type", msg.Type)
			}
		}
	}
}

// reconnectWithBackoff attempts to reconnect with exponential backoff
func (e *EventsAPI) reconnectWithBackoff() {
	delay := eventsReconnectDelay

	for {
		e.reconnectAttempt++

		// if e.config.MaxRetries > 0 && e.reconnectAttempt > e.config.MaxRetries {
		// 	slog.Error("socketmgr maximum reconnection attempts reached", "attempts", e.reconnectAttempt)
		// 	return
		// }

		// Check if we've been cancelled
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(delay):
			// Continue with reconnect
		}

		slog.Info("socketmgr attempting reconnection", "attempt", e.reconnectAttempt, "delay", delay)

		ctx, cancel := context.WithTimeout(e.ctx, eventsReconnectTimeout)

		e.mu.Lock()
		wsClient, err := e.connectLocked(ctx)
		e.mu.Unlock()

		cancel()

		if err == nil {
			go e.manageConnection(wsClient)
			return
		}

		// Add some jitter to the delay
		delay = min(delay*2, eventsMaxReconnectDelay)
		jitterFactor := 0.75 + (rand.Float64() * 0.5)
		delay = time.Duration(float64(delay) * jitterFactor)
	}
}

// fullURL builds the complete WebSocket URL with query parameters
func (e *EventsAPI) fullURL() (string, error) {
	// get base url from client
	baseURL, err := url.JoinPath(e.client.BaseURL, eventsPath)
	if err != nil {
		return "", fmt.Errorf("failed to join path: %w", err)
	}
	// get query params from client
	queryParams := e.client.QueryParams
	// append query params to base url
	fullUrl := baseURL + "?" + queryParams.Encode()

	return toWebsocketURL(fullUrl), nil
}

// toWebsocketURL converts an HTTP URL to a WebSocket URL
func toWebsocketURL(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "wss://" + url[8:]
	} else if strings.HasPrefix(url, "http://") {
		return "ws://" + url[7:]
	}
	return url
}
