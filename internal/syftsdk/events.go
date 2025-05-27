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
	"github.com/openmined/syftbox/internal/syftmsg"
	"resty.dev/v3"
)

const (
	eventsBufferSize        = 16
	eventsReconnectDelay    = 1 * time.Second
	eventsMaxReconnectDelay = 8 * time.Second
	eventsReconnectTimeout  = 10 * time.Second
	wsClientMaxMessageSize  = 4 * 1024 * 1024 // 4MB
	eventsPath              = "/api/v1/events"
)

// EventsAPI manages real-time event communication
type EventsAPI struct {
	client           *resty.Client
	wsClient         *wsClient
	messages         chan *syftmsg.Message
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.RWMutex
	connected        bool
	reconnectAttempt int
}

// newEventsAPI creates a new EventsAPI instance
func newEventsAPI(client *resty.Client) *EventsAPI {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventsAPI{
		client:   client,
		ctx:      ctx,
		cancel:   cancel,
		messages: make(chan *syftmsg.Message, eventsBufferSize),
	}
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

// Send sends a message through the WebSocket
func (e *EventsAPI) Send(msg *syftmsg.Message) error {
	e.mu.RLock()
	wsClient := e.wsClient
	connected := e.connected
	e.mu.RUnlock()

	if !connected || wsClient == nil {
		return ErrEventsNotConnected
	}

	select {
	case wsClient.msgTx <- msg:
		slog.Debug("socketmgr tx", "id", msg.Id, "type", msg.Type)
		return nil
	default:
		return ErrEventsMessageQueueFull
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

	// Connect with auth
	headers := e.client.Header()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: headers, // this will include the auth token
	})
	if err != nil {
		return nil, fmt.Errorf("sdk: events: failed to connect to %s: %w", url, err)
	}
	conn.SetReadLimit(wsClientMaxMessageSize)

	// Create and start client
	wsClient := newWSClient(conn)
	wsClient.Start(e.ctx)

	e.wsClient = wsClient
	e.connected = true

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
				slog.Debug("socketmgr rx closed")
				return
			}

			slog.Debug("socketmgr rx", "id", msg.Id, "type", msg.Type)

			select {
			case e.messages <- msg:
				// Successfully delivered
			default:
				slog.Warn("socketmgr rx buffer full. dropped", "id", msg.Id, "type", msg.Type)
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
	baseURL, err := url.JoinPath(e.client.BaseURL(), eventsPath)
	if err != nil {
		return "", fmt.Errorf("failed to join path: %w", err)
	}
	// get query params from client
	queryParams := e.client.QueryParams()
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
