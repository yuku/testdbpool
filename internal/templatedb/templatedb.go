package templatedb

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuku/testdbpool/internal/pgconst"
)

// TemplateDB represents the template database.
type TemplateDB struct {
	// cfg is the configuration for this TemplateDB instance.
	cfg *Config

	// name is the name of the template database.
	name string
}

type Config struct {
	// PoolID is the ID of the pool that this template database belongs to.
	PoolID string

	// ConnPool is the pgxpool.Pool to use for root database connections.
	ConnPool *pgxpool.Pool

	// DatabaseOwner specifies the owner for the template and test databases.
	// If empty, uses the default owner (connection user).
	DatabaseOwner string
}

// New creates a new TemplateDB instance with the given configuration.
// It validates that the template database name is valid but does NOT create the database.
// The template database must already exist before using this instance.
func New(cfg *Config) (*TemplateDB, error) {
	name, err := getTemplateDatabaseName(cfg.PoolID)
	if err != nil {
		return nil, fmt.Errorf("invalid template database name: %w", err)
	}
	return &TemplateDB{
		cfg:  cfg,
		name: name,
	}, nil
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
// The template database must already exist before calling this method.
func (t *TemplateDB) Create(ctx context.Context, name string) (*pgxpool.Pool, error) {
	// Check if the template database exists
	var exists bool
	err := t.cfg.ConnPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, t.name).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if template database exists: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("template database %s does not exist; please create it before using testdbpool", t.name)
	}

	// Check if the target database already exists
	err = t.cfg.ConnPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if target database exists: %w", err)
	}

	// If the database already exists, skip creation
	if !exists {
		if err := t.createFromTemplate(ctx, name); err != nil {
			return nil, fmt.Errorf("failed to create database from template: %w", err)
		}
	}

	cfg := t.cfg.ConnPool.Config().Copy()
	cfg.ConnConfig.Database = name
	return pgxpool.NewWithConfig(ctx, cfg)
}

func (t *TemplateDB) createFromTemplate(ctx context.Context, name string) error {
	var query string
	if t.cfg.DatabaseOwner != "" {
		query = fmt.Sprintf(
			`CREATE DATABASE %s OWNER %s TEMPLATE %s`,
			pgx.Identifier{name}.Sanitize(),
			pgx.Identifier{t.cfg.DatabaseOwner}.Sanitize(),
			t.SanitizedName(),
		)
	} else {
		query = fmt.Sprintf(
			`CREATE DATABASE %s TEMPLATE %s`,
			pgx.Identifier{name}.Sanitize(), t.SanitizedName(),
		)
	}

	_, err := t.cfg.ConnPool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create database from template: %w", err)
	}
	return nil
}

// Cleanup drops the template database and releases any resources.
func (t *TemplateDB) Cleanup(ctx context.Context) error {
	// Check if the template database exists
	var exists bool
	err := t.cfg.ConnPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, t.name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if template database exists: %w", err)
	}
	if !exists {
		return nil // Template database does not exist, nothing to clean up
	}

	// To drop the template database, we need to first alter it to not be a template
	// and then drop it.
	_, err = t.cfg.ConnPool.Exec(ctx, fmt.Sprintf(
		`ALTER DATABASE %s IS_TEMPLATE false`, t.SanitizedName(),
	))
	if err != nil {
		return fmt.Errorf("failed to alter template database: %w", err)
	}

	_, err = t.cfg.ConnPool.Exec(ctx, fmt.Sprintf(
		`DROP DATABASE IF EXISTS %s`, t.SanitizedName(),
	))
	if err != nil {
		return fmt.Errorf("failed to drop template database: %w", err)
	}
	return nil
}
