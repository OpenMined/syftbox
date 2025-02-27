package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/yashgorana/syftbox-go/pkg/acl"
	"github.com/yashgorana/syftbox-go/pkg/blob"
	"github.com/yashgorana/syftbox-go/pkg/datasite"
	"github.com/yashgorana/syftbox-go/pkg/server/v1/ws"
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

	<-ctx.Done()
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
