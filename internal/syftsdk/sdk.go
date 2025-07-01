package syftsdk

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openmined/syftbox/internal/utils"
	"github.com/openmined/syftbox/internal/version"
	"resty.dev/v3"
)

const (
	RetryInterval        = 5 * time.Second
	TokenRefreshInterval = 24 * time.Hour
)

// SyftSDK is the main client for interacting with the Syft API
type SyftSDK struct {
	config   *SyftSDKConfig
	client   *resty.Client
	Datasite *DatasiteAPI
	Blob     *BlobAPI
	Events   *EventsAPI

	onAuthTokenUpdate func(refreshToken string)
}

// New creates a new SyftSDK client
func New(config *SyftSDKConfig) (*SyftSDK, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sdk config: %w", err)
	}

	client := resty.New().
		SetBaseURL(config.BaseURL).
		SetTLSClientConfig(&tls.Config{
			MinVersion: tls.VersionTLS13,
		}).
		SetRetryCount(3).
		SetRetryWaitTime(1*time.Second).
		SetHeader(HeaderUserAgent, "SyftBox/"+version.Version).
		SetHeader(HeaderSyftVersion, version.Version).
		SetHeader(HeaderSyftDeviceId, utils.HWID).
		SetHeader(HeaderSyftUser, config.Email).
		SetQueryParam("user", config.Email).
		SetRetryMaxWaitTime(RetryInterval).
		AddContentTypeEncoder("json", jsonEncoder).
		AddContentTypeDecoder("json", jsonDecoder)

	datasiteAPI := newDatasiteAPI(client)
	blobAPI := newBlobAPI(client)
	eventsAPI := newEventsAPI(client)

	return &SyftSDK{
		config:   config,
		client:   client,
		Datasite: datasiteAPI,
		Blob:     blobAPI,
		Events:   eventsAPI,
	}, nil
}

// Close terminates all connections and cleans up resources
func (s *SyftSDK) Close() {
	if s.Events.IsConnected() {
		s.Events.Close()
	}
	s.client.Close()
}

// Authenticate sets the user authentication for API calls and events
func (s *SyftSDK) Authenticate(ctx context.Context) error {
	if isAuthDisabled() || isDevURL(s.config.BaseURL) {
		slog.Warn("sdk auth disabled, skipping auth")
		return nil
	}

	// if we have an access token, set it
	if s.config.AccessToken != "" {
		slog.Debug("sdk using existing access token")
		if err := s.setAccessToken(s.config.AccessToken); err != nil {
			return err
		}
	} else {
		// we have no access token, refresh auth tokens once
		if err := s.refreshAuthToken(ctx); err != nil {
			return err
		}
	}

	// periodically refresh auth tokens
	go s.autoRefreshToken(ctx)

	return nil
}

func (s *SyftSDK) OnAuthTokenUpdate(fn func(refreshToken string)) {
	s.onAuthTokenUpdate = fn
}

func (s *SyftSDK) autoRefreshToken(ctx context.Context) {
	ticker := time.NewTicker(TokenRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Info("auto refreshing auth token")
			if err := s.refreshAuthToken(ctx); err != nil {
				slog.Error("auto refresh auth token", "error", err)
			}
		}
	}
}

func (s *SyftSDK) refreshAuthToken(ctx context.Context) error {
	slog.Debug("sdk refreshing auth tokens")

	refreshToken, err := ParseToken(s.config.RefreshToken, RefreshToken)
	if err != nil {
		return fmt.Errorf("parse refresh token: %w", err)
	}
	if err := refreshToken.Validate(s.config.Email, s.config.BaseURL); err != nil {
		return fmt.Errorf("validate refresh token: %w", err)
	}

	// refresh auth tokens with current refresh token
	resp, err := RefreshAuthTokens(ctx, s.config.BaseURL, s.config.RefreshToken)
	if err != nil {
		return err
	}

	// set access token
	if err := s.setAccessToken(resp.AccessToken); err != nil {
		return err
	}

	// notify callback
	if s.onAuthTokenUpdate != nil {
		s.onAuthTokenUpdate(resp.RefreshToken)
	}

	return nil
}

func (s *SyftSDK) setAccessToken(accessToken string) error {
	// validate access token
	claims, err := ParseToken(accessToken, AccessToken)

	if err != nil {
		return fmt.Errorf("failed to parse access token: %w", err)
	}

	if err := claims.Validate(s.config.Email, s.config.BaseURL); err != nil {
		return fmt.Errorf("access token: %w", err)
	}

	// set access token
	s.client.SetAuthScheme("Bearer")
	s.client.SetAuthToken(accessToken)

	slog.Debug("sdk update access token", "user", claims.Subject, "expiry", claims.ExpiresAt)
	return nil
}

func isAuthDisabled() bool {
	authEnabled := os.Getenv("SYFTBOX_AUTH_ENABLED")
	enabled, err := strconv.ParseBool(authEnabled)
	if err != nil {
		return false
	}
	return !enabled
}

func isDevURL(baseURL string) bool {
	return strings.Contains(baseURL, "localhost") ||
		strings.Contains(baseURL, "127.0.0.1") ||
		strings.Contains(baseURL, "0.0.0.0")
}
