package sync

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/client/subscriptions"
)

type SubscriptionManager struct {
	path    string
	lastMod time.Time
	cfg     *subscriptions.Config
	mu      sync.Mutex
}

func NewSubscriptionManager(path string) *SubscriptionManager {
	return &SubscriptionManager{
		path: path,
	}
}

func (m *SubscriptionManager) Get() *subscriptions.Config {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, err := os.Stat(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.cfg = subscriptions.DefaultConfig()
			m.lastMod = time.Time{}
			return m.cfg
		}
		if m.cfg != nil {
			slog.Warn("subscriptions stat failed, using cached config", "path", m.path, "error", err)
			return m.cfg
		}
		m.cfg = subscriptions.DefaultConfig()
		return m.cfg
	}

	if m.cfg != nil && !info.ModTime().After(m.lastMod) {
		return m.cfg
	}

	cfg, err := subscriptions.Load(m.path)
	if err != nil {
		if m.cfg != nil {
			slog.Warn("subscriptions load failed, using cached config", "path", m.path, "error", err)
			return m.cfg
		}
		slog.Warn("subscriptions load failed, using default config", "path", m.path, "error", err)
		m.cfg = subscriptions.DefaultConfig()
		return m.cfg
	}

	m.cfg = cfg
	m.lastMod = info.ModTime()
	return m.cfg
}

func (m *SubscriptionManager) Path() string {
	return m.path
}
