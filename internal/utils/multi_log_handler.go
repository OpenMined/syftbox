package utils

import (
	"context"
	"log/slog"
)

// MultiLogHandler implements slog.Handler interface and forwards logs to multiple handlers
type MultiLogHandler struct {
	handlers []slog.Handler
}

// NewMultiLogHandler creates a new MultiLogHandler that forwards logs to multiple handlers
func NewMultiLogHandler(handlers ...slog.Handler) *MultiLogHandler {
	return &MultiLogHandler{
		handlers: handlers,
	}
}

// Enabled implements slog.Handler
func (h *MultiLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements slog.Handler
func (h *MultiLogHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if e := handler.Handle(ctx, r); e != nil {
				err = e
			}
		}
	}
	return err
}

// WithAttrs implements slog.Handler
func (h *MultiLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return NewMultiLogHandler(handlers...)
}

// WithGroup implements slog.Handler
func (h *MultiLogHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return NewMultiLogHandler(handlers...)
}
