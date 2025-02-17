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
)

type Server struct {
	config *Config
	server *http.Server

	blobSvc     *blob.BlobStorageService
	aclSvc      *acl.AclService
	datasiteSvc *datasite.DatasiteService
}

func New(config *Config) (*Server, error) {
	blobSvc := blob.NewBlobStorageService(config.Blob)
	aclSvc := acl.NewAclService()
	datasiteSvc := datasite.NewDatasiteService(blobSvc, aclSvc)

	return &Server{
		config:      config,
		blobSvc:     blobSvc,
		aclSvc:      aclSvc,
		datasiteSvc: datasiteSvc,
		server: &http.Server{
			Addr:    config.Http.Addr,
			Handler: SetupRoutes(datasiteSvc),
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {

	slog.Info("datasite service start")
	if err := s.datasiteSvc.Init(ctx); err != nil {
		return fmt.Errorf("datasite service start error: %w", err)
	}

	go func() error {
		if err := s.runHttpServer(); err != nil && err != http.ErrServerClosed {
			slog.Error("server start error", "error", err)
			return err
		}
		slog.Debug("http server stopped")
		return nil
	}()

	<-ctx.Done()
	slog.Info("server shutdown signal")
	if err := s.Stop(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
		return err
	}
	slog.Info("stopped")
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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
