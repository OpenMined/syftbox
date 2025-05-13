package db

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/openmined/syftbox/internal/utils"
)

// SQLite pragmas for optimal performance
const defaultPragma = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA temp_store=MEMORY;
PRAGMA cache_size=8000;
PRAGMA mmap_size=268435456;
`

// config holds internal configuration for DB creation
type config struct {
	path            string
	pragmas         string
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}

// SqliteOption defines a function that configures the DB
type SqliteOption func(*config)

// WithPath sets the path for the SQLite database
// Use ":memory:" for an in-memory database
func WithPath(path string) SqliteOption {
	return func(c *config) {
		c.path = path
	}
}

// WithPragmas sets custom pragmas for the SQLite connection
// This replaces the default pragmas
func WithPragmas(pragmas string) SqliteOption {
	return func(c *config) {
		c.pragmas = pragmas
	}
}

// WithMaxOpenConns sets the maximum number of open connections
func WithMaxOpenConns(n int) SqliteOption {
	return func(c *config) {
		c.maxOpenConns = n
	}
}

// WithMaxIdleConns sets the maximum number of idle connections
func WithMaxIdleConns(n int) SqliteOption {
	return func(c *config) {
		c.maxIdleConns = n
	}
}

// WithConnMaxLifetime sets the maximum lifetime of a connection
func WithConnMaxLifetime(d time.Duration) SqliteOption {
	return func(c *config) {
		c.connMaxLifetime = d
	}
}

// NewSqliteDb creates a new sqlx.DB with the provided options
func NewSqliteDb(opts ...SqliteOption) (*sqlx.DB, error) {
	// Default configuration
	cfg := &config{
		path:         ":memory:",
		pragmas:      defaultPragma,
		maxOpenConns: 0, // Default is unlimited
		maxIdleConns: 2, // Default is 2
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	// Ensure parent directory exists for file-based DBs
	var dsn string
	if cfg.path != ":memory:" {
		if err := utils.EnsureParent(cfg.path); err != nil {
			return nil, fmt.Errorf("ensure parent directory: %w", err)
		}
		dsn = fmt.Sprintf("file:%s?_txlock=immediate&mode=rwc", cfg.path)
	} else {
		dsn = ":memory:"
	}

	// Connect to the database
	slog.Info("db", "driver", driverName, "path", cfg.path)
	db, err := sqlx.Connect(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	// Set connection pool parameters
	if cfg.maxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.maxOpenConns)
	}
	if cfg.maxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.maxIdleConns)
	}
	if cfg.connMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.connMaxLifetime)
	}

	if _, err := db.Exec(cfg.pragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	return db, nil
}
