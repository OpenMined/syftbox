package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/yashgorana/syftbox-go/pkg/blob"
)

type Server struct {
	config *Config
	server *http.Server
}

func New(config *Config) (*Server, error) {

	cfg := aws.Config{
		Credentials: credentials.NewStaticCredentialsProvider(config.Blob.AccessKey, config.Blob.SecretKey, ""),
		Region:      config.Blob.Region,
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(config.Blob.ServerUrl)
		o.UsePathStyle = true
	})

	blobService := blob.NewBlobAPI(client, config.Blob.BucketName)
	return &Server{
		config: config,
		server: &http.Server{
			Addr:    config.Http.Addr,
			Handler: SetupRoutes(blobService),
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
	if s.config.Http.CertFile != "" && s.config.Http.KeyFile != "" {
		slog.Info("server start tls", "addr", s.config.Http.Addr, "cert", s.config.Http.CertFile, "key", s.config.Http.KeyFile)
		return s.server.ListenAndServeTLS(s.config.Http.CertFile, s.config.Http.KeyFile)
	} else {
		slog.Info("server start http", "addr", s.config.Http.Addr)
		return s.server.ListenAndServe()
	}
}
