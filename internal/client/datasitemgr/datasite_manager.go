package datasitemgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasite"
	"github.com/openmined/syftbox/internal/utils"
)

var (
	ErrDatasiteAlreadyStarted = errors.New("datasite already started")
	ErrDatasiteNotStarted     = errors.New("datasite not started")
	ErrConfigIsNil            = errors.New("config is nil")
)

type DatasiteManagerOpts func(*DatasiteManager)

type DatasiteManager struct {
	datasite    *datasite.Datasite
	status      DatasiteStatus
	clientURL   string
	datasiteErr error
	mu          sync.RWMutex
}

func New() *DatasiteManager {
	ds := &DatasiteManager{
		status: DatasiteStatusUnprovisioned,
	}
	return ds
}

func (d *DatasiteManager) SetClientURL(clientURL string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clientURL = clientURL
}

func (d *DatasiteManager) Start(ctx context.Context) error {
	slog.Info("datasite manager start")

	if !d.defaultConfigExists() {
		slog.Info("default config not found. waiting to be provisioned.")
		return nil
	}

	slog.Info("default config found. provisioning datasite.")
	cfg, err := config.LoadFromFile(config.DefaultConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// if this fails, it means the datasite was provisioned with a bad config
	// but it can be provisioned again, so don't bubble up the error
	if err := d.newDatasite(ctx, cfg); err != nil {
		slog.Error("datasite start", "error", err)
	}

	return nil
}

func (d *DatasiteManager) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.datasite != nil {
		d.datasite.Stop()
		d.datasite = nil
	}

	slog.Info("datasite manager stopped")
}

func (d *DatasiteManager) Get() (*datasite.Datasite, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.datasite == nil {
		return nil, ErrDatasiteNotStarted
	}

	return d.datasite, nil
}

func (d *DatasiteManager) Status() *DatasiteManagerStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return &DatasiteManagerStatus{
		Status:        d.status,
		DatasiteError: d.datasiteErr,
		Datasite:      d.datasite,
	}
}

func (d *DatasiteManager) Provision(config *config.Config) error {
	return d.newDatasite(context.Background(), config)
}

func (d *DatasiteManager) newDatasite(ctx context.Context, cfg *config.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cfg == nil {
		return ErrConfigIsNil
	}

	if d.datasite != nil {
		return ErrDatasiteAlreadyStarted
	}

	// patch config to use the correct client URL
	if d.clientURL != "" {
		cfg.ClientURL = d.clientURL
	}

	d.status = DatasiteStatusProvisioning
	d.datasiteErr = nil

	// create datasite
	newDs, err := datasite.New(cfg)
	if err != nil {
		d.datasiteErr = err
		d.status = DatasiteStatusError
		return fmt.Errorf("create datasite: %w", err)
	}

	d.datasite = newDs

	go func() {
		if err := d.datasite.Start(ctx); err != nil {
			slog.Error("start datasite", "error", err)
			d.datasite.Stop()
			d.datasite = nil
			d.datasiteErr = err
			d.status = DatasiteStatusError
		} else {
			d.status = DatasiteStatusProvisioned
		}
	}()

	return nil
}

func (d *DatasiteManager) defaultConfigExists() bool {
	return utils.FileExists(config.DefaultConfigPath)
}
