package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
	config        *Config
	server        *http.Server
	db            *sqlx.DB
	hub           *ws.WebsocketHub
	svc           *Services
	manifestStore *ws.ManifestStore
	hotlinkStore  *hotlinkStore
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
	manifestStore := ws.NewManifestStore()
	hotlinkStore := newHotlinkStore()
	httpHandler := SetupRoutes(config, services, hub)

	return &Server{
		config:        config,
		db:            sqliteDb,
		hub:           hub,
		svc:           services,
		manifestStore: manifestStore,
		hotlinkStore:  hotlinkStore,
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
	case syftmsg.MsgACLManifest:
		s.handleACLManifest(msg)
	case syftmsg.MsgHotlinkOpen:
		s.handleHotlinkOpen(msg)
	case syftmsg.MsgHotlinkAccept:
		s.handleHotlinkAccept(msg)
	case syftmsg.MsgHotlinkReject:
		s.handleHotlinkReject(msg)
	case syftmsg.MsgHotlinkData:
		s.handleHotlinkData(msg)
	case syftmsg.MsgHotlinkClose:
		s.handleHotlinkClose(msg)
	case syftmsg.MsgHotlinkSignal:
		s.handleHotlinkSignal(msg)
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

	if latencyTraceEnabled() {
		if ts, ok := payloadTimestampNs(data.Content); ok {
			slog.Info("latency_trace server_received", msgGroup, "age_ms", (time.Now().UnixNano()-ts)/1_000_000)
		}
	}

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
		start := time.Now()
		if _, err := s.svc.Blob.Backend().PutObject(context.Background(), &blob.PutObjectParams{
			Key:  data.Path,
			ETag: msg.Message.Id,
			Body: bytes.NewReader(data.Content),
			Size: data.Length,
		}); err != nil {
			slog.Error("ws file write put object", "error", err)
			return err
		}
		if latencyTraceEnabled() {
			slog.Info("latency_trace server_uploaded", msgGroup, "upload_ms", time.Since(start).Milliseconds())
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

const latencyTraceEnv = "SYFTBOX_LATENCY_TRACE"

func latencyTraceEnabled() bool {
	return os.Getenv(latencyTraceEnv) == "1"
}

func payloadTimestampNs(payload []byte) (int64, bool) {
	if len(payload) < 8 {
		return 0, false
	}
	return int64(binary.LittleEndian.Uint64(payload[:8])), true
}

func (s *Server) handleACLManifest(msg *ws.ClientMessage) {
	manifest, ok := msg.Message.Data.(*syftmsg.ACLManifest)
	if !ok {
		slog.Error("wsmsg handler invalid manifest data type")
		return
	}

	from := msg.ClientInfo.User
	msgGroup := slog.Group("wsmsg", "id", msg.Message.Id, "type", msg.Message.Type, "connId", msg.ConnID, "from", from, "datasite", manifest.Datasite, "for", manifest.For, "forHash", manifest.ForHash)

	// Only accept manifests from the datasite owner
	if manifest.Datasite != from {
		slog.Warn("wsmsg manifest rejected - not owner", msgGroup)
		return
	}

	// Store the manifest
	s.manifestStore.Store(manifest)
	slog.Info("wsmsg manifest stored", msgGroup, "aclCount", len(manifest.ACLOrder))

	// Route the manifest to the appropriate user(s)
	s.hub.BroadcastFiltered(msg.Message, func(info *ws.ClientInfo) bool {
		to := info.User

		// Don't send back to sender
		if to == from {
			return false
		}

		// Check if this manifest is for this user
		toHash := syftmsg.HashPrincipal(to)
		if manifest.ForHash == toHash || manifest.ForHash == "public" {
			slog.Info("wsmsg manifest routed", msgGroup, "to", to)
			return true
		}

		return false
	})
}

func (s *Server) handleHotlinkOpen(msg *ws.ClientMessage) {
	open, ok := msg.Message.Data.(syftmsg.HotlinkOpen)
	if !ok {
		slog.Error("hotlink open invalid payload", "msgId", msg.Message.Id)
		return
	}

	from := msg.ClientInfo.User
	msgGroup := slog.Group("hotlink", "id", msg.Message.Id, "session", open.SessionID, "from", from, "path", open.Path)

	if err := s.svc.ACL.CanAccess(
		acl.NewRequest(open.Path, &acl.User{ID: from}, acl.AccessWrite),
	); err != nil {
		slog.Warn("hotlink open permission denied", msgGroup, "error", err)
		s.hub.SendMessage(msg.ConnID, syftmsg.NewHotlinkReject(open.SessionID, "permission denied"))
		return
	}

	s.hotlinkStore.Open(open.SessionID, open.Path, from, msg.ConnID)

	s.hub.BroadcastFiltered(msg.Message, func(info *ws.ClientInfo) bool {
		to := info.User
		if to == from {
			return false
		}
		if err := s.svc.ACL.CanAccess(
			acl.NewRequest(open.Path, &acl.User{ID: to}, acl.AccessRead),
		); err != nil {
			return false
		}
		return true
	})
}

func (s *Server) handleHotlinkAccept(msg *ws.ClientMessage) {
	accept, ok := msg.Message.Data.(syftmsg.HotlinkAccept)
	if !ok {
		slog.Error("hotlink accept invalid payload", "msgId", msg.Message.Id)
		return
	}

	session, ok := s.hotlinkStore.Get(accept.SessionID)
	if !ok {
		s.hub.SendMessage(msg.ConnID, syftmsg.NewHotlinkReject(accept.SessionID, "unknown session"))
		return
	}

	if msg.ClientInfo.User == session.FromUser {
		return
	}

	if err := s.svc.ACL.CanAccess(
		acl.NewRequest(session.Path, &acl.User{ID: msg.ClientInfo.User}, acl.AccessRead),
	); err != nil {
		s.hub.SendMessage(msg.ConnID, syftmsg.NewHotlinkReject(accept.SessionID, "permission denied"))
		return
	}

	s.hotlinkStore.Accept(accept.SessionID, msg.ConnID, msg.ClientInfo.User)
	s.hub.SendMessage(session.FromConn, msg.Message)
}

func (s *Server) handleHotlinkReject(msg *ws.ClientMessage) {
	reject, ok := msg.Message.Data.(syftmsg.HotlinkReject)
	if !ok {
		slog.Error("hotlink reject invalid payload", "msgId", msg.Message.Id)
		return
	}

	session, ok := s.hotlinkStore.Get(reject.SessionID)
	if !ok {
		return
	}

	if msg.ClientInfo.User == session.FromUser {
		return
	}

	s.hub.SendMessage(session.FromConn, msg.Message)
}

func (s *Server) handleHotlinkData(msg *ws.ClientMessage) {
	data, ok := msg.Message.Data.(syftmsg.HotlinkData)
	if !ok {
		slog.Error("hotlink data invalid payload", "msgId", msg.Message.Id)
		return
	}

	if latencyTraceEnabled() {
		if ts, ok := payloadTimestampNs(data.Payload); ok {
			path := strings.TrimSpace(data.Path)
			slog.Info("latency_trace hotlink_server_received", "path", path, "age_ms", (time.Now().UnixNano()-ts)/1_000_000, "size", len(data.Payload))
		}
	}

	session, ok := s.hotlinkStore.Get(data.SessionID)
	if !ok {
		s.hub.SendMessage(msg.ConnID, syftmsg.NewHotlinkReject(data.SessionID, "unknown session"))
		return
	}

	if msg.ClientInfo.User != session.FromUser {
		slog.Warn("hotlink data rejected (not sender)", "session", data.SessionID, "from", msg.ClientInfo.User)
		return
	}

	for connID, user := range session.Accepted {
		if err := s.svc.ACL.CanAccess(
			acl.NewRequest(session.Path, &acl.User{ID: user}, acl.AccessRead),
		); err != nil {
			continue
		}
		s.hub.SendMessage(connID, msg.Message)
	}
}

func (s *Server) handleHotlinkClose(msg *ws.ClientMessage) {
	closeMsg, ok := msg.Message.Data.(syftmsg.HotlinkClose)
	if !ok {
		slog.Error("hotlink close invalid payload", "msgId", msg.Message.Id)
		return
	}

	session, ok := s.hotlinkStore.Get(closeMsg.SessionID)
	if !ok {
		return
	}

	if msg.ClientInfo.User == session.FromUser {
		if session, ok := s.hotlinkStore.Close(closeMsg.SessionID); ok {
			for connID := range session.Accepted {
				s.hub.SendMessage(connID, msg.Message)
			}
		}
		return
	}

	s.hotlinkStore.RemoveAccepted(closeMsg.SessionID, msg.ConnID)
	s.hub.SendMessage(session.FromConn, msg.Message)
}

func (s *Server) handleHotlinkSignal(msg *ws.ClientMessage) {
	signal, ok := msg.Message.Data.(syftmsg.HotlinkSignal)
	if !ok {
		slog.Error("hotlink signal invalid payload", "msgId", msg.Message.Id)
		return
	}

	session, ok := s.hotlinkStore.Get(signal.SessionID)
	if !ok {
		s.hub.SendMessage(msg.ConnID, syftmsg.NewHotlinkSignal(signal.SessionID, "quic_error", nil, "", "unknown session"))
		return
	}

	isSender := msg.ClientInfo.User == session.FromUser
	if !isSender {
		if _, ok := session.Accepted[msg.ConnID]; !ok {
			slog.Warn("hotlink signal rejected (not participant)", "session", signal.SessionID, "from", msg.ClientInfo.User)
			return
		}
	}

	if isSender {
		for connID := range session.Accepted {
			s.hub.SendMessage(connID, msg.Message)
		}
		return
	}

	s.hub.SendMessage(session.FromConn, msg.Message)
}
