package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
)

var (
	appScanInterval = 5 * time.Second
	ErrAppNotFound  = errors.New("app not found")
)

type AppScheduler struct {
	manager       *AppManager
	configPath    string
	runningApps   map[string]*App
	runningAppsWg sync.WaitGroup
	mu            sync.RWMutex
}

func NewAppScheduler(mgr *AppManager, configPath string) *AppScheduler {
	return &AppScheduler{
		manager:     mgr,
		configPath:  configPath,
		runningApps: make(map[string]*App),
	}
}

func (s *AppScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	slog.Info("scheduler start", "appdir", s.manager.AppsDir)

	go func() {
		ticker := time.NewTicker(appScanInterval)
		defer ticker.Stop()

		s.scanApps() // first scan
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.scanApps()
			}
		}
	}()

	return nil
}

func (s *AppScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	slog.Debug("scheduler stopping")
	s.stopAllAppsLocked()
	s.runningAppsWg.Wait()
	slog.Debug("scheduler stopped")
}

func (s *AppScheduler) GetApp(appId string) (*App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	app, ok := s.runningApps[appId]
	if !ok {
		return nil, ErrAppNotFound
	}

	return app, nil
}

func (s *AppScheduler) StartApp(appId string) (*App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	app, ok := s.runningApps[appId]
	if !ok {
		return nil, ErrAppNotFound
	}

	if app.GetStatus() == StatusRunning {
		return app, ErrAlreadyRunning
	}

	go func() {
		if err := s.startAppLifecycle(app); err != nil {
			slog.Error("scheduler failed to start app", "app", app.Info().ID, "error", err)
		}
	}()

	return app, nil
}

func (s *AppScheduler) StopApp(appId string) (*App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	app, ok := s.runningApps[appId]
	if !ok {
		return nil, ErrAppNotFound
	}

	return app, app.Stop()
}

func (s *AppScheduler) ListRunningApps() []*App {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Collect(maps.Values(s.runningApps))
}

func (s *AppScheduler) scanApps() {
	scheduled := 0
	removed := 0

	// list all apps
	appList, err := s.manager.ListApps()
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		return
	}

	// create a map of apps by id
	apps := make(map[string]*AppInfo)
	for _, app := range appList {
		apps[app.ID] = app
	}

	// if app exists, but is not running, schedule it
	for appID, app := range apps {
		if _, ok := s.runningApps[appID]; !ok {
			go func() {
				if err := s.scheduleApp(app); err != nil {
					slog.Error("scheduler failed to schedule app", "app", appID, "error", err)
				}
			}()
			scheduled++
		}
	}

	// stop + remove apps that are no longer in the list
	for appID := range s.runningApps {
		if _, ok := apps[appID]; !ok {
			if err := s.removeApp(appID); err != nil {
				slog.Error("scheduler remove app error", "app", appID, "error", err)
			}
			removed++
		}
	}

	// slog.Debug("scheduler scan apps", "installed", len(appList), "running", len(s.runningApps), "scheduled", scheduled, "removed", removed)
}

func (s *AppScheduler) scheduleApp(appInfo *AppInfo) error {
	app, err := NewApp(appInfo, s.configPath)
	if err != nil {
		slog.Error("failed to create app", "app", appInfo.ID, "error", err)
		return err
	}

	s.mu.Lock()
	s.runningApps[appInfo.ID] = app
	s.mu.Unlock()

	return s.startAppLifecycle(app)
}

func (s *AppScheduler) startAppLifecycle(app *App) error {
	if app == nil {
		return fmt.Errorf("app is nil")
	}

	appId := app.Info().ID

	s.runningAppsWg.Add(1)
	defer s.runningAppsWg.Done()

	if err := app.Start(); err != nil {
		slog.Error("scheduler failed to start app", "app", appId, "error", err)
		return err
	}
	slog.Info("scheduler started app", "app", appId, "pid", app.Process().Pid, "port", app.port)

	code, err := app.Wait()
	if err != nil {
		switch code {
		case 137:
			slog.Warn("scheduler app exited with SIGKILL", "app", appId)
		case 143:
			// sigterm
			slog.Warn("scheduler app exited with SIGTERM", "app", appId)
		default:
			slog.Warn("scheduler app exited", "app", appId, "exitcode", code, "error", err)
		}
	} else {
		slog.Info("scheduler app exited", "app", appId, "exitcode", code)
	}
	// introduce retry logic over here
	return nil
}

func (s *AppScheduler) removeApp(appID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// stop the app
	app, ok := s.runningApps[appID]
	if !ok {
		return nil
	}

	delete(s.runningApps, appID)
	slog.Debug("scheduler removed app", "app", appID)
	return app.Stop()
}

func (s *AppScheduler) stopAllAppsLocked() {
	for _, app := range s.runningApps {
		if app.GetStatus() == StatusRunning {
			slog.Info("scheduler stopping app", "app", app.info.ID)
			if err := app.Stop(); err != nil {
				slog.Error("failed to stop app", "app", app.info.ID, "error", err)
			}
		}
	}
}
