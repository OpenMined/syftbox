package server

import (
	"bytes"
	"context"
	"crypto/tls"
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
	sqliteDb, err := db.NewSqliteDB(
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
	httpHandler := SetupRoutes(config, services, hub)

	return &Server{
		config: config,
		db:     sqliteDb,
		hub:    hub,
		svc:    services,
		server: &http.Server{
			Addr:    config.HTTP.Addr,
			Handler: httpHandler,
			// Timeouts to prevent slow client attacks
			ReadTimeout:       config.HTTP.ReadTimeout,
			WriteTimeout:      config.HTTP.WriteTimeout,
			IdleTimeout:       config.HTTP.IdleTimeout,
			ReadHeaderTimeout: config.HTTP.ReadHeaderTimeout,
			// Connection control
			MaxHeaderBytes: 1 << 20, // 1 MB,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // TLS 1.2 or higher
			},
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
	if s.config.HTTP.HTTPSEnabled() {
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
			slog.Debug("server received websocket message", "msgType", msg.Message.Type, "msgId", msg.Message.Id, "connId", msg.ConnID, "from", msg.ClientInfo.User)
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
	if err := s.svc.ACL.CanAccess(
		acl.NewRequest(data.Path, &acl.User{ID: from}, acl.AccessWrite),
	); err != nil {
		slog.Error("wsmsg handler permission denied", msgGroup, "error", err)
		errMsg := syftmsg.NewError(http.StatusForbidden, data.Path, "permission denied for write operation")
		s.hub.SendMessage(msg.ConnID, errMsg)
		return
	}

	slog.Info("wsmsg handler recieved", msgGroup)

	// Check if this is an ACL file
	isACLFile := strings.HasSuffix(data.Path, "/syft.pub.yaml") || data.Path == "syft.pub.yaml"

	// For ACL files, upload synchronously to ensure they're loaded before broadcasting
	// For other files, upload asynchronously for better performance
	uploadFunc := func() error {
		if _, err := s.svc.Blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
			Key:  data.Path,
			ETag: msg.Message.Id,
			Body: bytes.NewReader(data.Content),
			Size: data.Length,
		}); err != nil {
			slog.Error("ws file write put object", "error", err)
			return err
		}
		return nil
	}

	if isACLFile {
		// Synchronous upload for ACL files - wait for upload + ACL loading before broadcasting
		if err := uploadFunc(); err != nil {
			// Send NACK to sender
			nackMsg := syftmsg.NewNack(msg.Message.Id, err.Error())
			s.hub.SendMessage(msg.ConnID, nackMsg)
			return
		}

		// Load the ACL immediately after upload to ensure it's available for permission checks
		// This is more reliable than waiting for the async blob change callback
		ruleSet, err := s.svc.ACL.LoadACLFromContent(data.Path, data.Content)
		if err != nil {
			slog.Error("ws file write ACL parse error", msgGroup, "error", err)
		} else if ruleSet != nil {
			if _, err := s.svc.ACL.AddRuleSet(ruleSet); err != nil {
				slog.Error("ws file write ACL add ruleset error", msgGroup, "error", err)
			} else {
				slog.Info("ws file write ACL loaded synchronously", msgGroup, "path", ruleSet.Path)
			}
		}

		// Send ACK to sender after successful ACL file write
		ackMsg := syftmsg.NewAck(msg.Message.Id)
		s.hub.SendMessage(msg.ConnID, ackMsg)
	} else {
		// Asynchronous upload for regular files - send ACK/NACK when done
		go func() {
			if err := uploadFunc(); err != nil {
				// Send NACK to sender
				nackMsg := syftmsg.NewNack(msg.Message.Id, err.Error())
				s.hub.SendMessage(msg.ConnID, nackMsg)
			} else {
				// Send ACK to sender after successful file write
				ackMsg := syftmsg.NewAck(msg.Message.Id)
				s.hub.SendMessage(msg.ConnID, ackMsg)
			}
		}()
	}

	// broadcast the message to all clients except the sender
	s.hub.BroadcastFiltered(msg.Message, func(info *ws.ClientInfo) bool {
		to := info.User

		if to == from {
			slog.Debug("wsmsg handler skip self", msgGroup)
			return false
		}

		// ACL files (syft.pub.yaml) are metadata - always broadcast them
		// This prevents chicken-and-egg problem where ACL can't be read because it hasn't synced yet
		isACLFile := strings.HasSuffix(data.Path, "/syft.pub.yaml") || data.Path == "syft.pub.yaml"

		if isACLFile {
			slog.Info("wsmsg handler ACL bypass", msgGroup, "to", to, "reason", "ACL metadata file")
		} else {
			// check if the RECIPIENT has permission to read the file
			if err := s.svc.ACL.CanAccess(
				acl.NewRequest(data.Path, &acl.User{ID: to}, acl.AccessRead),
			); err != nil {
				slog.Warn("wsmsg handler permission denied", msgGroup, "to", to, "error", err)
				return false
			}
		}

		slog.Info("wsmsg handler broadcast", msgGroup, "to", to)

		return true
	})
}
