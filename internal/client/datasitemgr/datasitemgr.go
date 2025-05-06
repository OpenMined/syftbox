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
	ds        *datasite.Datasite
	mu        sync.RWMutex
	clientURL string
}

func New(opts ...DatasiteManagerOpts) *DatasiteManger {
	ds := &DatasiteManger{}
	for _, opt := range opts {
		opt(ds)
	}
	return ds
}

func (d *DatasiteManger) SetClientURL(clientURL string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clientURL = clientURL
}

func (d *DatasiteManger) Start(ctx context.Context) error {
	slog.Info("datasite manager start")
	if d.defaultConfigExists() {
		cfg, err := config.LoadClientConfig(config.DefaultConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
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

	// patch config to use the correct client URL
	if d.clientURL != "" {
		cfg.ClientURL = d.clientURL
	}

	// create datasite
	ds, err := datasite.New(cfg)
	if err != nil {
		return fmt.Errorf("create datasite: %w", err)
	}

	d.ds = ds

	go func() {
		if err := ds.Start(ctx); err != nil {
			slog.Error("start datasite", "error", err)
			d.ds = nil
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
