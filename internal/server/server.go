package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yashgorana/syftbox-go/internal/acl"
	"github.com/yashgorana/syftbox-go/internal/blob"
	"github.com/yashgorana/syftbox-go/internal/datasite"
	"github.com/yashgorana/syftbox-go/internal/message"
	"github.com/yashgorana/syftbox-go/internal/server/v1/ws"
)

type Server struct {
	config *Config
	server *http.Server
	hub    *ws.WebsocketHub

	blobSvc     *blob.BlobService
	aclSvc      *acl.AclService
	datasiteSvc *datasite.DatasiteService
}

func New(config *Config) (*Server, error) {
	aclSvc := acl.NewAclService()
	blobSvc := blob.NewBlobService(config.Blob)
	datasiteSvc := datasite.NewDatasiteService(blobSvc, aclSvc)

	hub := ws.NewHub()
	httpHandler := SetupRoutes(hub, blobSvc, datasiteSvc)

	return &Server{
		config:      config,
		blobSvc:     blobSvc,
		aclSvc:      aclSvc,
		datasiteSvc: datasiteSvc,
		hub:         hub,
		server: &http.Server{
			Addr:    config.Http.Addr,
			Handler: httpHandler,
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	slog.Info("syftgo server start")
	defer slog.Info("syftgo server stop")

	slog.Info("datasite service start")
	if err := s.datasiteSvc.Init(ctx); err != nil {
		return fmt.Errorf("datasite service start error: %w", err)
	}

	go s.hub.Run(ctx)

	go func() error {
		if err := s.runHttpServer(); err != nil && err != http.ErrServerClosed {
			slog.Error("server start error", "error", err)
			return err
		}
		slog.Info("http server stopped")
		return nil
	}()

	var workerWg sync.WaitGroup

	go func() {
		numWorkers := runtime.NumCPU()
		workerWg.Add(numWorkers)
		slog.Info("message handlers start", "count", numWorkers)

		for range numWorkers {
			go func() {
				defer workerWg.Done()
				s.handleSocketMessages(ctx)
			}()
		}
	}()

	<-ctx.Done()
	workerWg.Wait()
	slog.Info("syftgo shutdown signal")
	if err := s.Stop(ctx); err != nil {
		slog.Error("syftgo shutdown error", "error", err)
		return err
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	s.hub.Shutdown(ctx)

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}

func (s *Server) runHttpServer() error {
	if s.config.Http.CertFile != "" && s.config.Http.KeyFile != "" {
		slog.Info("server start tls", "addr", s.config.Http.Addr, "cert", s.config.Http.CertFile, "key", s.config.Http.KeyFile)
		return s.server.ListenAndServeTLS(s.config.Http.CertFile, s.config.Http.KeyFile)
	} else {
		slog.Info("server start http", "addr", s.config.Http.Addr)
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
	case message.MsgFileWrite:
		s.handleFileWrite(msg)
	// case message.MsgFileDelete:
	// 	s.handleFileDelete(msg)
	default:
		slog.Info("unhandled message", "msgType", msg.Message.Type)
	}
}

func (s *Server) handleFileWrite(msg *ws.ClientMessage) {
	data, _ := msg.Message.Data.(message.FileWrite)

	from := msg.Info.User
	isRpc := false

	// todo workaround for new rpc
	if strings.HasSuffix(data.Path, ".request") || strings.HasSuffix(data.Path, ".response") {
		isRpc = true
	}

	// check permissions
	ok, err := s.aclSvc.CanAccess(from, &acl.FileInfo{Path: data.Path, Size: data.Length}, acl.ActionFileWrite)
	if !isRpc && (!ok || err != nil) {
		slog.Warn("FILE_WRITE permissions error", "msgId", msg.Message.Id, "from", from, "path", data.Path, "err", err)
		errMsg := message.NewError(http.StatusForbidden, data.Path, "no permissions to write the file")
		s.hub.SendMessage(msg.ClientId, errMsg)
		return
	}

	slog.Info("FILE_WRITE", "client", from, "msgId", msg.Message.Id, "path", data.Path, "size", data.Length)

	s.hub.BroadcastFiltered(msg.Message, func(info *ws.ClientInfo) bool {
		to := info.User
		if to == from {
			return false
		} else if isRpc {
			return true
		}

		ok, err := s.aclSvc.CanAccess(to, &acl.FileInfo{Path: data.Path, Size: data.Length}, acl.ActionFileRead)
		if !ok || err != nil {
			return false
		}
		return true
	})
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
