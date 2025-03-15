package syftapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/yashgorana/syftbox-go/internal/message"
)

const (
	reconnectInitialDelay = 1 * time.Second
	reconnectMaxDelay     = 30 * time.Second
	maxMessageSize        = 1024 * 1024 // 1MB
)

// websocketManager handles WebSocket connections and message distribution
type websocketManager struct {
	url         string
	wsClient    *websocketClient
	subscribers map[chan *message.Message]bool
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	connected   bool
	// Authentication
	userID      string
	authHeaders map[string]string
}

// newWebsocketManager creates a new WebSocket manager with the given URL
func newWebsocketManager(wsURL string) *websocketManager {
	url := convertToWebsocketURL(wsURL)
	ctx, cancel := context.WithCancel(context.Background())

	return &websocketManager{
		url:         url,
		subscribers: make(map[chan *message.Message]bool),
		ctx:         ctx,
		cancel:      cancel,
		authHeaders: make(map[string]string),
	}
}

// SetUser sets the user ID for authentication via query parameter
func (m *websocketManager) SetUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userID = userID

	// Reconnect if already connected to apply new auth
	if m.connected && m.wsClient != nil {
		go func() {
			ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			defer cancel()

			// Disconnect first
			m.wsClient.Close()
			m.wsClient = nil
			m.connected = false

			// Reconnect with new auth
			if err := m.Connect(ctx); err != nil {
				slog.Error("failed to reconnect after auth change", "error", err)
			}
		}()
	}
}

// SetAuthHeader sets an authorization header for future JWT implementation
func (m *websocketManager) SetAuthHeader(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.authHeaders[key] = value

	// Reconnect if already connected to apply new auth
	if m.connected && m.wsClient != nil {
		go func() {
			ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			defer cancel()

			// Disconnect first
			m.wsClient.Close()
			m.wsClient = nil
			m.connected = false

			// Reconnect with new auth
			if err := m.Connect(ctx); err != nil {
				slog.Error("failed to reconnect after auth change", "error", err)
			}
		}()
	}
}

// Connect initiates a WebSocket connection
func (m *websocketManager) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected && m.wsClient != nil {
		return nil
	}

	wsClient, err := m.connectLocked(ctx)
	if err != nil {
		return err
	}

	go m.manageConnection(wsClient)
	return nil
}

// IsConnected returns the current connection status
func (m *websocketManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Subscribe creates a new message subscription channel
func (m *websocketManager) Subscribe() chan *message.Message {
	ch := make(chan *message.Message, 10) // Buffered channel to prevent drops

	m.mu.Lock()
	m.subscribers[ch] = true
	connected := m.connected
	m.mu.Unlock()

	if !connected {
		go func() {
			ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			defer cancel()

			if err := m.Connect(ctx); err != nil {
				slog.Error("connect failed during subscription", "error", err)
			}
		}()
	}

	return ch
}

// Unsubscribe removes and closes a subscription channel
func (m *websocketManager) Unsubscribe(ch chan *message.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.subscribers[ch]; exists {
		delete(m.subscribers, ch)
		close(ch)
	}
}

// SendMessage sends a message through the WebSocket
func (m *websocketManager) SendMessage(message *message.Message) error {
	m.mu.RLock()
	wsClient := m.wsClient
	connected := m.connected
	m.mu.RUnlock()

	if !connected || wsClient == nil {
		return errors.New("websocket not connected")
	}

	select {
	case wsClient.MsgTx <- message:
		return nil
	default:
		return errors.New("message queue is full")
	}
}

// Close terminates the WebSocket connection and cleans up
func (m *websocketManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cancel()

	if m.wsClient != nil {
		m.wsClient.Close()
		m.wsClient = nil
	}

	for ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = make(map[chan *message.Message]bool)
	m.connected = false

	slog.Info("websocket manager closed")
}

// connectLocked creates a new WebSocket connection (must be called with lock held)
func (m *websocketManager) connectLocked(ctx context.Context) (*websocketClient, error) {
	if m.wsClient != nil {
		m.wsClient.Close()
		m.wsClient = nil
		m.connected = false
	}

	// Build URL with auth query parameters if needed
	url := m.url
	if m.userID != "" {
		if strings.Contains(url, "?") {
			url += "&user=" + m.userID
		} else {
			url += "?user=" + m.userID
		}
	}

	// Setup auth headers for future JWT implementation
	header := make(map[string][]string)
	for k, v := range m.authHeaders {
		header[k] = []string{v}
	}

	// Connect with auth
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return nil, err
	}

	conn.SetReadLimit(maxMessageSize)
	wsClient := newWebsocketClient(conn)
	wsClient.Start(m.ctx)

	m.wsClient = wsClient
	m.connected = true

	slog.Info("websocket connected")
	return wsClient, nil
}

// manageConnection handles the WebSocket connection lifecycle
func (m *websocketManager) manageConnection(wsClient *websocketClient) {
	go m.distributeMessages(wsClient)

	select {
	case <-wsClient.Closed:
		slog.Info("websocket disconnected, will reconnect")

		m.mu.Lock()
		if m.wsClient == wsClient {
			m.wsClient = nil
			m.connected = false
		}
		m.mu.Unlock()

		select {
		case <-m.ctx.Done():
			return
		default:
			m.reconnectWithBackoff()
		}

	case <-m.ctx.Done():
		return
	}
}

// distributeMessages sends messages to all subscribers
func (m *websocketManager) distributeMessages(wsClient *websocketClient) {
	for {
		select {
		case msg, ok := <-wsClient.MsgRx:
			if !ok {
				return
			}

			m.mu.RLock()
			for ch := range m.subscribers {
				select {
				case ch <- msg:
					// Message delivered
				default:
					slog.Warn("subscriber buffer full, message dropped")
				}
			}
			m.mu.RUnlock()

		case <-wsClient.Closed:
			return

		case <-m.ctx.Done():
			return
		}
	}
}

// reconnectWithBackoff attempts to reconnect with exponential backoff
func (m *websocketManager) reconnectWithBackoff() {
	delay := reconnectInitialDelay

	for attempt := 1; ; attempt++ {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		time.Sleep(delay)
		slog.Info("attempting reconnection", "attempt", attempt, "delay", delay)

		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)

		m.mu.Lock()
		wsClient, err := m.connectLocked(ctx)
		m.mu.Unlock()

		cancel()

		if err == nil {
			go m.manageConnection(wsClient)
			return
		}

		slog.Error("reconnection failed", "error", err)

		delay *= 2
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
}

// convertToWebsocketURL converts HTTP URLs to WebSocket URLs
func convertToWebsocketURL(url string) string {
	switch {
	case strings.HasPrefix(url, "ws://"), strings.HasPrefix(url, "wss://"):
		return url
	case strings.HasPrefix(url, "http://"):
		return "ws://" + url[7:]
	case strings.HasPrefix(url, "https://"):
		return "wss://" + url[8:]
	default:
		return "wss://" + url
	}
}
