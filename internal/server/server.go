package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/openmined/syftbox/internal/db"
	"github.com/openmined/syftbox/internal/server/acl"
	"github.com/openmined/syftbox/internal/server/blob"
	"github.com/openmined/syftbox/internal/server/datasite"
	"github.com/openmined/syftbox/internal/server/handlers/ws"
	"github.com/openmined/syftbox/internal/syftmsg"
	"golang.org/x/sync/errgroup"
)

const (
	shutdownTimeout = 10 * time.Second
)

// Server represents the main application server and its dependencies
type Server struct {
	config *Config
	server *http.Server
	db     *sqlx.DB
	hub    *ws.WebsocketHub
	svc    *Services
}

// New creates a new server instance with the provided configuration
func New(config *Config) (*Server, error) {
	dbPath := filepath.Join(config.DataDir, "state.db")
	sqliteDb, err := db.NewSqliteDb(
		db.WithPath(dbPath),
		db.WithMaxOpenConns(runtime.NumCPU()),
	)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	services, err := NewServices(config, sqliteDb)
	if err != nil {
		return nil, fmt.Errorf("initialize services: %w", err)
	}

	hub := ws.NewHub()
	httpHandler := SetupRoutes(services, hub)

	return &Server{
		config: config,
		db:     sqliteDb,
		hub:    hub,
		svc:    services,
		server: &http.Server{
			Addr:    config.HTTP.Addr,
			Handler: httpHandler,
			// Timeouts to prevent slow client attacks
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			// Connection control
			MaxHeaderBytes: 1 << 20, // 1 MB
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	slog.Info("syftbox server start")

	// Create errgroup with derived context
	eg, egCtx := errgroup.WithContext(ctx)

	// Start services
	if err := s.svc.Start(egCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	// Start HTTP server
	eg.Go(func() error {
		if err := s.runHttpServer(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		slog.Info("http server stopped")
		return nil
	})

	// Start websocket hub
	eg.Go(func() error {
		s.hub.Run(egCtx)
		return nil
	})

	// Start socket message handlers
	numWorkers := runtime.NumCPU()
	slog.Info("message handlers start", "workers", numWorkers)
	for range numWorkers {
		eg.Go(func() error {
			s.handleSocketMessages(egCtx)
			return nil
		})
	}

	// Launch goroutine to handle shutdown on context cancellation
	eg.Go(func() error {
		<-egCtx.Done()
		slog.Info("context cancelled, starting shutdown")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.Stop(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
			return err
		}
		return nil
	})

	// Wait for all goroutines to complete or error
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("syftbox server failure", "error", err)
		return err
	}

	slog.Info("syftbox server stop")
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	// Use a timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	// Stop components in reverse order of startup
	var errs error

	// Shutdown hub
	s.hub.Shutdown(shutdownCtx)

	// Shutdown HTTP server
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		errs = errors.Join(errs, fmt.Errorf("http server shutdown: %w", err))
	}
	slog.Info("http server stopped")

	if err := s.svc.Shutdown(shutdownCtx); err != nil {
		errs = errors.Join(errs, fmt.Errorf("stop services: %w", err))
	}
	slog.Info("services stopped")

	// Close database connection
	if err := s.db.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("database close: %w", err))
	}
	slog.Info("database closed")

	if errs != nil {
		return fmt.Errorf("shutdown errors: %w", errs)
	}

	return nil
}

func (s *Server) runHttpServer() error {
	if s.config.HTTP.HasCerts() {
		slog.Info("server start https",
			"addr", fmt.Sprintf("https://%s", s.config.HTTP.Addr),
			"cert", s.config.HTTP.CertFilePath,
			"key", s.config.HTTP.KeyFilePath,
		)
		return s.server.ListenAndServeTLS(s.config.HTTP.CertFilePath, s.config.HTTP.KeyFilePath)
	} else {
		slog.Info("server start http", "addr", fmt.Sprintf("http://%s", s.config.HTTP.Addr))
		return s.server.ListenAndServe()
	}
}

func (s *Server) handleSocketMessages(ctx context.Context) {
	for {
		select {
		case msg := <-s.hub.Messages():
			s.onMessage(msg)

		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) onMessage(msg *ws.ClientMessage) {
	switch msg.Message.Type {
	case syftmsg.MsgFileWrite:
		s.handleFileWrite(msg)
	// case message.MsgFileDelete:
	// 	s.handleFileDelete(msg)
	default:
		slog.Info("unhandled message", "msgType", msg.Message.Type)
	}
}

func (s *Server) handleFileWrite(msg *ws.ClientMessage) {
	// unwrap the data
	data, _ := msg.Message.Data.(syftmsg.FileWrite)
	// get the client info
	from := msg.ClientInfo.User

	msgGroup := slog.Group("wsmsg", "id", msg.Message.Id, "type", msg.Message.Type, "connId", msg.ConnID, "from", from, "path", data.Path, "size", data.Length)

	// check if the SENDER has permission to write to the file
	if err := s.checkPermission(from, data.Path, acl.AccessWrite); err != nil {
		slog.Error("wsmsg handler permission denied", msgGroup, "error", err)
		errMsg := syftmsg.NewError(http.StatusForbidden, data.Path, "permission denied for write operation")
		s.hub.SendMessage(msg.ConnID, errMsg)
		return
	}

	slog.Info("wsmsg handler recieved", msgGroup)

	// broadcast the message to all clients except the sender
	s.hub.BroadcastFiltered(msg.Message, func(info *ws.ClientInfo) bool {
		to := info.User

		if to == from {
			slog.Debug("wsmsg handler skip self", msgGroup)
			return false
		}

		// check if the RECIPIENT has permission to read the file
		if err := s.checkPermission(to, data.Path, acl.AccessRead); err != nil {
			slog.Warn("wsmsg handler permission denied", msgGroup, "to", to, "error", err)
			return false
		} else {
			slog.Info("wsmsg handler broadcast", msgGroup, "to", to)
		}

		return true
	})

	if _, err := s.svc.Blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
		Key:  data.Path,
		ETag: msg.Message.Id,
		Body: bytes.NewReader(data.Content),
		Size: data.Length,
	}); err != nil {
		slog.Error("ws file write put object", "error", err)
	}
}

func (s *Server) checkPermission(user string, path string, access acl.AccessLevel) error {
	// todo remove hax once perms can be updated through sync
	if isRpc(path) {
		return nil
	}
	return s.svc.ACL.CanAccess(
		&acl.User{ID: user, IsOwner: datasite.IsOwner(path, user)},
		&acl.File{Path: path},
		access,
	)
}

func isRpc(path string) bool {
	return strings.Contains(path, "/rpc/") &&
		(strings.HasSuffix(path, ".request") ||
			strings.HasSuffix(path, ".response") ||
			strings.HasSuffix(path, "rpc.schema.json"))
}

// func (s *Server) handleFileDelete(msg *ws.ClientMessage) {
// 	slog.Info("FILE_DELETE", "client", msg.Info.User, "msgId", msg.Message.Id)
// }

// func datasiteOwner(path string) string {
// 	parts := strings.Split(path, "/")
// 	if len(parts) < 2 {
// 		return ""
// 	}
// 	return parts[1]
// }
