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
	manager    *AppManager
	configPath string
	sched      map[string]*App
	schedWg    sync.WaitGroup
	schedMu    sync.RWMutex
}

func NewAppScheduler(mgr *AppManager, configPath string) *AppScheduler {
	return &AppScheduler{
		manager:    mgr,
		configPath: configPath,
		sched:      make(map[string]*App),
	}
}

// Start the scheduler
func (s *AppScheduler) Start(ctx context.Context) error {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
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

// Stop the scheduler
func (s *AppScheduler) Stop() {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()

	slog.Debug("scheduler stopping")
	s.stopAllAppsUnsafe()
	s.schedWg.Wait()
	slog.Debug("scheduler stopped")
}

// Get an app
func (s *AppScheduler) GetApp(appId string) (*App, bool) {
	s.schedMu.RLock()
	defer s.schedMu.RUnlock()

	app, ok := s.sched[appId]
	if !ok {
		return nil, false
	}

	return app, true
}

// Start an app
func (s *AppScheduler) StartApp(appId string) (*App, error) {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()

	app, ok := s.sched[appId]
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
	s.schedMu.Lock()
	defer s.schedMu.Unlock()

	app, ok := s.sched[appId]
	if !ok {
		return nil, ErrAppNotFound
	}

	return app, app.Stop()
}

func (s *AppScheduler) GetApps() []*App {
	s.schedMu.RLock()
	defer s.schedMu.RUnlock()

	return slices.Collect(maps.Values(s.sched))
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

	s.schedMu.RLock()
	scheduledApps := s.sched
	s.schedMu.RUnlock()

	// if app exists, but is not running, schedule it
	for appID, app := range apps {
		if _, ok := scheduledApps[appID]; !ok {
			if err := s.scheduleApp(app); err != nil {
				slog.Error("scheduler failed to schedule app", "app", appID, "error", err)
			}
			scheduled++
		}
	}

	// stop + remove apps that are no longer in the list
	for appID := range scheduledApps {
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

	s.schedMu.Lock()
	defer s.schedMu.Unlock()

	if _, ok := s.sched[appInfo.ID]; ok {
		return nil // app is already scheduled
	}

	s.sched[appInfo.ID] = app // add to scheduler

	go func() {
		if err := s.startAppLifecycle(app); err != nil {
			slog.Error("scheduler failed to start app", "app", app.Info().ID, "error", err)
		}
	}()

	return nil
}

func (s *AppScheduler) startAppLifecycle(app *App) error {
	if app == nil {
		return fmt.Errorf("app is nil")
	}

	// add to wait group
	s.schedWg.Add(1)
	defer s.schedWg.Done()

	// start the app
	appId := app.Info().ID
	if err := app.Start(); err != nil {
		slog.Error("scheduler failed to start app", "app", appId, "error", err)
		return err
	}
	slog.Info("scheduler started app", "app", appId, "pid", app.Process().Pid, "port", app.port)

	// wait for the app to exit
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

	return nil
}

func (s *AppScheduler) removeApp(appID string) error {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	// stop the app
	app, ok := s.sched[appID]
	if !ok {
		return nil
	}

	delete(s.sched, appID)
	slog.Debug("scheduler removed app", "app", appID)
	return app.Stop()
}

// stopAllAppsUnsafe stops all apps without locking the scheduler
// this is used to avoid deadlocks for recursive calls
func (s *AppScheduler) stopAllAppsUnsafe() {
	for _, app := range s.sched {
		if app.GetStatus() == StatusRunning {
			slog.Info("scheduler stopping app", "app", app.Info().ID)
			if err := app.Stop(); err != nil {
				slog.Error("failed to stop app", "app", app.Info().ID, "error", err)
			}
		}
	}
}
