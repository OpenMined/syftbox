package localhttp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yashgorana/syftbox-go/internal/localhttp/controllers"
	_ "github.com/yashgorana/syftbox-go/internal/localhttp/docs" // use underscore to only run the init function
	"github.com/yashgorana/syftbox-go/internal/localhttp/middleware"
	"github.com/yashgorana/syftbox-go/internal/localhttp/services"
	"github.com/yashgorana/syftbox-go/internal/utils"

	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

// Annotations for Swagger docs generation. Update with care.
//	@title			SyftBox UI Bridge API
//	@version		0.1.0
//	@description	API bridge server for SyftUI

//	@BasePath					/
//	@securityDefinitions.apikey	APIToken
//	@in							header
//	@name						Authorization

// Server represents a UI bridge server instance
type Server struct {
	config   Config
	engine   *gin.Engine
	server   *http.Server
	listener net.Listener
}

// New creates a new UI bridge server with the given configuration
func New(config Config) (*Server, error) {
	// If token is not provided, generate a random one
	if config.Token == "" {
		config.Token = utils.TokenHex(16)
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// NOTE middleware order is important
	engine.Use(
		middleware.ErrorHandler(), // Error handling should be first to catch all errors
		gin.Recovery(),            // Recovery should be early to catch panics
		middleware.RequestLogger(),
		middleware.CORSMiddleware(),
		middleware.Compression(middleware.DefaultCompressionConfig()),
	)

	// Apply rate limiter middelware if configured
	if config.RateLimit > 0 {
		rateLimit := config.RateLimit
		rateBurst := config.RateLimitBurst
		if rateBurst <= 0 {
			rateBurst = 5 // Default burst size
		}

		engine.Use(middleware.RateLimit(middleware.RateLimiterConfig{
			RequestsPerSecond: rateLimit,
			BurstSize:         rateBurst,
		}))
	}

	// Apply timeout middleware if configured
	if config.RequestTimeout > 0 {
		// Create a map of paths that should be excluded from timeout
		excludedPaths := map[string]bool{
			"/health": true, // Health endpoint should not time out
		}

		engine.Use(middleware.Timeout(middleware.TimeoutConfig{
			Timeout:       config.RequestTimeout,
			ExcludedPaths: excludedPaths,
		}))
	}

	return &Server{
		config: config,
		engine: engine,
	}, nil
}

// Starts the UI bridge server
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Enabled {
		slog.Info("UI bridge server is disabled")
		return nil
	}

	// Register routes
	s.registerRoutes()

	// Enable Swagger documentation if configured
	if s.config.EnableSwagger {
		s.engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// Create the HTTP server
	s.server = &http.Server{
		Handler: s.engine,
	}

	// Listen on the specified port
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	// Get the actual port (important when using port 0)
	addr = listener.Addr().String()
	slog.Info("Starting UI bridge server", "address", addr)

	// Output the SyftBox access information
	host, port, _ := net.SplitHostPort(addr)
	fmt.Printf("\nUI bridge server running at: http://%s\n", addr)
	fmt.Printf("Token: %s\n\n", s.config.Token)
	fmt.Printf("To access SyftBox, open this URL in a browser:\n    https://syftbox.openmined.org/#host=%s&port=%s&token=%s\n\n",
		host, port, s.config.Token)
	if s.config.EnableSwagger {
		fmt.Printf("Swagger UI: http://%s/swagger/index.html\n\n", addr)
	}

	// Start the server in a goroutine
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("UI bridge server error", "error", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return s.Stop()
}

// Stop stops the UI bridge server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}

	slog.Info("Stopping UI bridge server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to stop UI bridge server: %w", err)
	}

	return nil
}

// registerRoutes sets up all routes for the UI bridge server
func (s *Server) registerRoutes() {
	// Create services
	healthService := services.NewHealthService()
	statusService := services.NewStatusService()

	// Register health controller (unauthenticated)
	healthController := controllers.NewHealthController(healthService)
	healthController.RegisterRoutes(s.engine)

	// Auth middleware for protected routes
	authConfig := middleware.TokenAuthConfig{
		Token: s.config.Token,
	}

	// API v1 routes (authenticated)
	v1 := s.engine.Group("/v1")
	v1.Use(middleware.TokenAuth(authConfig))

	// Register status controller (authenticated)
	statusController := controllers.NewStatusController(statusService)
	statusController.RegisterRoutes(v1)
}
