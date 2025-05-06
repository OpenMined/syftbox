package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openmined/syftbox/internal/client/datasitemgr"
	"github.com/openmined/syftbox/internal/client/middleware"
	"github.com/openmined/syftbox/internal/utils"
)

type ControlPlaneServer struct {
	config      *ControlPlaneConfig
	server      *http.Server
	datasiteMgr *datasitemgr.DatasiteManger
	url         string
}

func NewControlPlaneServer(config *ControlPlaneConfig, datasiteMgr *datasitemgr.DatasiteManger) (*ControlPlaneServer, error) {
	if config.AuthToken == "" {
		config.AuthToken = utils.TokenHex(16)
	}

	cpURL, err := addrToURL(config.Addr)
	if err != nil {
		return nil, fmt.Errorf("convert address to URL: %w", err)
	}

	datasiteMgr.SetClientURL(cpURL)

	routes := SetupRoutes(datasiteMgr, &RouteConfig{
		Swagger:         config.EnableSwagger,
		ControlPlaneURL: cpURL,
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
		url:         cpURL,
	}, nil
}

func (s *ControlPlaneServer) Start(ctx context.Context) error {
	slog.Info("control plane start", "addr", s.url, "token", s.config.AuthToken)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func (s *ControlPlaneServer) Stop(ctx context.Context) error {
	slog.Info("control plane stop")
	return s.server.Shutdown(ctx)
}

func addrToURL(addr string) (string, error) {
	// this is not the most robust solution. but it's good enough.
	// if we're facing any issues, perhaps simplify the addr we're passing in?
	if strings.HasSuffix(addr, ":") {
		return "", fmt.Errorf("bad address: %s", addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parse address: %w", err)
	}

	if host == "" {
		host = "0.0.0.0"
	}

	if port == "" {
		port = "80"
	}

	url := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
	}

	return url.String(), nil
}
