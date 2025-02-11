package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type Server struct {
	config *ServerConfig
	server *http.Server
}

func New(config *ServerConfig) (*Server, error) {
	return &Server{
		config: config,
		server: &http.Server{
			Addr:    config.Addr,
			Handler: SetupRoutes(),
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
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
	if s.config.CertFile != "" && s.config.KeyFile != "" {
		slog.Info("server start tls", "addr", s.config.Addr, "cert", s.config.CertFile, "key", s.config.KeyFile)
		return s.server.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	} else {
		slog.Info("server start http", "addr", s.config.Addr)
		return s.server.ListenAndServe()
	}
}
