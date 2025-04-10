package syftapi

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/yashgorana/syftbox-go/internal/syftmsg"
)

const (
	reconnectInitialDelay = 1 * time.Second
	reconnectMaxDelay     = 8 * time.Second
	maxMessageSize        = 4 * 1024 * 1024
)

var (
	ErrWebsocketNotConnected = errors.New("websocket not connected")
	ErrMessageQueueFull      = errors.New("message queue is full")
)

// websocketManager handles WebSocket connections and message distribution
type websocketManager struct {
	url       string
	wsClient  *websocketClient
	messages  chan *syftmsg.Message
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	connected bool

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
		ctx:         ctx,
		cancel:      cancel,
		messages:    make(chan *syftmsg.Message, 256),
		authHeaders: make(map[string]string),
	}
}

// SetUser sets the user ID for authentication via query parameter
func (m *websocketManager) SetUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userID = userID
}

// SetAuthHeader sets an authorization header for future JWT implementation
func (m *websocketManager) SetAuthHeader(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.authHeaders[key] = value
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

func (m *websocketManager) Messages() <-chan *syftmsg.Message {
	return m.messages
}

// SendMessage sends a message through the WebSocket
func (m *websocketManager) SendMessage(message *syftmsg.Message) error {
	m.mu.RLock()
	wsClient := m.wsClient
	connected := m.connected
	m.mu.RUnlock()

	if !connected || wsClient == nil {
		return ErrWebsocketNotConnected
	}

	select {
	case wsClient.MsgTx <- message:
		return nil
	default:
		return ErrMessageQueueFull
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
	// todo clean this up later
	url := m.url
	if m.userID != "" {
		url += "?user=" + m.userID
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
	go m.consumeMessages(wsClient)

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

func (m *websocketManager) consumeMessages(wsClient *websocketClient) {
	for {
		select {
		case <-m.ctx.Done():
			return

		case <-wsClient.Closed:
			return

		case msg, ok := <-wsClient.MsgRx:
			if !ok {
				slog.Debug("ws manager - client read channe closed")
				return
			}
			// Blocking send - will wait until the message can be sent
			m.messages <- msg
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

		// Exponential backoff
		delay *= 2
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}

		// Add jitter to prevent synchronized retry spikes
		// Creates a random jitter value between 0 and 25% of the current delay
		// Subtracts a small fixed portion (12.5%) of the delay, then adds the jitter
		jitter := time.Duration(rand.Float64() * float64(delay/4))
		delay = delay - (delay / 8) + jitter
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
