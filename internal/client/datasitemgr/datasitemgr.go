package datasitemgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/openmined/syftbox/internal/client/config"
	"github.com/openmined/syftbox/internal/client/datasite"
)

var (
	ErrDatasiteAlreadyStarted = errors.New("datasite already started")
	ErrDatasiteNotStarted     = errors.New("datasite not started")
)

type DatasiteManagerOpts func(*DatasiteManger)

type DatasiteManger struct {
	ds     *datasite.Datasite
	config *config.Config
	mu     sync.RWMutex
}

func NewDatasiteManger(opts ...DatasiteManagerOpts) *DatasiteManger {
	ds := &DatasiteManger{}
	for _, opt := range opts {
		opt(ds)
	}
	return ds
}

func WithConfig(config *config.Config) DatasiteManagerOpts {
	return func(d *DatasiteManger) {
		d.config = config
	}
}

func (d *DatasiteManger) Start(ctx context.Context) error {
	slog.Info("datasite manager start")
	if d.config != nil {
		return d.newDatasite(ctx, d.config)
	} else if d.defaultConfigExists() {
		cfg, err := config.LoadClientConfig(config.DefaultConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return d.newDatasite(ctx, cfg)
	}

	return nil
}

func (d *DatasiteManger) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ds != nil {
		d.ds.Stop()
		d.ds = nil
	}

	slog.Info("datasite manager stopped")
}

func (d *DatasiteManger) Get() (*datasite.Datasite, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.ds == nil {
		return nil, ErrDatasiteNotStarted
	}

	return d.ds, nil
}

func (d *DatasiteManger) Provision(config *config.Config) error {
	return d.newDatasite(context.Background(), config)
}

func (d *DatasiteManger) newDatasite(ctx context.Context, cfg *config.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ds != nil {
		return ErrDatasiteAlreadyStarted
	}

	if cfg.Path == "" {
		cfg.Path = config.DefaultConfigPath
	}

	if err := cfg.Save(cfg.Path); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	ds, err := datasite.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create datasite: %w", err)
	}

	d.ds = ds
	d.config = cfg

	go func() {
		if err := ds.Start(ctx); err != nil {
			slog.Error("failed to start datasite", "error", err)
		}
	}()

	return nil
}

func (d *DatasiteManger) defaultConfigExists() bool {
	if _, err := os.Stat(config.DefaultConfigPath); os.IsNotExist(err) {
		return false
	} else {
		return err == nil
	}
}
