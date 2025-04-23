package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/middleware"
)

type ControlPlaneServer struct {
	config      *ControlPlaneConfig
	server      *http.Server
	datasiteMgr *datasitemgr.DatasiteManger
}

func NewControlPlaneServer(config *ControlPlaneConfig, datasiteMgr *datasitemgr.DatasiteManger) (*ControlPlaneServer, error) {
	routes := SetupRoutes(datasiteMgr, &RouteConfig{
		Swagger: config.EnableSwagger,
		Auth: middleware.TokenAuthConfig{
			Token: config.AuthToken,
		},
	})

	httpServer := &http.Server{
		Addr:    config.Addr,
		Handler: routes,
		// Timeouts to prevent slow client attacks
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		// Connection control
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	return &ControlPlaneServer{
		config:      config,
		server:      httpServer,
		datasiteMgr: datasiteMgr,
	}, nil
}

func (s *ControlPlaneServer) Start(ctx context.Context) error {
	slog.Info("control plane start", "addr", fmt.Sprintf("http://%s", s.config.Addr), "token", s.config.AuthToken)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func (s *ControlPlaneServer) Stop(ctx context.Context) error {
	slog.Info("control plane stop")
	return s.server.Shutdown(ctx)
}
