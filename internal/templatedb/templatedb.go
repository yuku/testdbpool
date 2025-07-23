package templatedb

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/testdbpool/internal/pgconst"
)

const (
	// lockID is the advisory lock ID used to ensure that only one testdbpool instance
	// can set up the template database at a time.
	lockID = 132435465768
)

// TemplateDB represents the template database.
type TemplateDB struct {
	// cfg is the configuration for this TemplateDB instance.
	cfg *Config

	// setup indicates whether the template database has been set up.
	setup bool

	// name is the name of the template database.
	name string

	// mu is a mutex to protect access to the template database setup.
	mu sync.Mutex
}

type Config struct {
	// PoolID is the ID of the pool that this template database belongs to.
	PoolID string

	// ConnPool is the pgxpool.Pool to use for root database connections.
	ConnPool *pgxpool.Pool

	// Setup is the function that sets up the template database.
	Setup func(context.Context, *pgx.Conn) error
}

// New creates a new TemplateDB instance with the given configuration.
func New(cfg *Config) (*TemplateDB, error) {
	name, err := getTemplateDatabaseName(cfg.PoolID)
	if err != nil {
		return nil, fmt.Errorf("invalid template database name: %w", err)
	}
	return &TemplateDB{
		cfg:   cfg,
		name:  name,
		setup: false,
	}, nil
}

// Setup sets up the template database using the provided Setup function.
// This method is idempotent; it will only set up the database if it has not
// been set up yet.
func (t *TemplateDB) Setup(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.setup {
		return nil // Template database already set up
	}

	err := pgx.BeginFunc(ctx, t.cfg.ConnPool, func(tx pgx.Tx) error {
		// Get advisory lock to ensure only one testdbpool instance sets up the
		// template database at a time.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
			return fmt.Errorf("failed to acquire advisory lock: %w", err)
		}

		// TODO: Provide a way to force recreation of the template database.
		if exists, err := checkIfExists(ctx, tx, t.name); err != nil {
			return fmt.Errorf("failed to check if template database exists: %w", err)
		} else if exists {
			t.setup = true
			return nil // Template database already exists
		}

		if err := t.createDatabase(ctx, t.SanitizedName()); err != nil {
			return fmt.Errorf("failed to create template database: %w", err)
		}

		conn, err := t.connect(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to template database: %w", err)
		}
		defer func() { _ = conn.Close(ctx) }()

		if err := t.cfg.Setup(ctx, conn); err != nil {
			return fmt.Errorf("failed to set up template database: %w", err)
		}
		t.setup = true

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func checkIfExists(ctx context.Context, tx pgx.Tx, name string) (bool, error) {
	var exists bool
	err := tx.
		QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, name).
		Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if template database exists: %w", err)
	}
	return exists, nil
}

func (t *TemplateDB) createDatabase(ctx context.Context, name string) error {
	// CREATE DATABASE cannot run inside a transaction block
	_, err := t.cfg.ConnPool.
		Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, name))
	if err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}
	return nil
}

func (t *TemplateDB) connect(ctx context.Context) (*pgx.Conn, error) {
	cfg := t.cfg.ConnPool.Config().ConnConfig.Copy()
	cfg.Database = t.name
	return pgx.ConnectConfig(ctx, cfg)
}

// Name returns the name of the template database.
func (t *TemplateDB) Name() string {
	return t.name
}

// SanitizedName returns the sanitized name of the template database.
// This is useful for safely using the name in SQL queries.
func (t *TemplateDB) SanitizedName() string {
	return pgx.Identifier{t.name}.Sanitize()
}

func getTemplateDatabaseName(id string) (string, error) {
	name := fmt.Sprintf("testdbpooltmpl_%s", id)
	if len(name) > pgconst.MaxDatabaseNameLength {
		return "", fmt.Errorf(
			"template database name exceeds maximum length of %d characters: %s",
			pgconst.MaxDatabaseNameLength, name,
		)
	}
	return name, nil
}

// Create creates a new database using the template database and returns a
// pgxpool.Pool connected to the new database.
func (t *TemplateDB) Create(ctx context.Context, name string) (*pgxpool.Pool, error) {
	if err := t.Setup(ctx); err != nil {
		return nil, fmt.Errorf("failed to set up template database: %w", err)
	}

	err := pgx.BeginFunc(ctx, t.cfg.ConnPool, func(tx pgx.Tx) error {
		// Get advisory lock to ensure only one testdbpool instance sets up the
		// template database at a time.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
			return fmt.Errorf("failed to acquire advisory lock: %w", err)
		}

		if exists, err := checkIfExists(ctx, tx, name); err != nil {
			return fmt.Errorf("failed to check if template database exists: %w", err)
		} else if exists {
			t.setup = true
			return nil // Template database already exists
		}

		if err := t.createFromTemplate(ctx, name); err != nil {
			return fmt.Errorf("failed to create template database: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	cfg := t.cfg.ConnPool.Config().Copy()
	cfg.ConnConfig.Database = name
	return pgxpool.NewWithConfig(ctx, cfg)
}

func (t *TemplateDB) createFromTemplate(ctx context.Context, name string) error {
	_, err := t.cfg.ConnPool.Exec(ctx, fmt.Sprintf(
		`CREATE DATABASE %s WITH TEMPLATE %s`,
		pgx.Identifier{name}.Sanitize(), t.SanitizedName(),
	))
	if err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}
	return nil
}

// Cleanup drops the template database and releases any resources.
func (t *TemplateDB) Cleanup(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.setup {
		return nil // Template database not set up, nothing to clean up
	}

	_, err := t.cfg.ConnPool.Exec(ctx, fmt.Sprintf(
		`DROP DATABASE IF EXISTS %s`, t.SanitizedName(),
	))
	if err != nil {
		return fmt.Errorf("failed to drop template database: %w", err)
	}
	t.setup = false
	return nil
}
