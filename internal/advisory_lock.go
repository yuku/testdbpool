package internal

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
)

// AdvisoryLock manages PostgreSQL advisory locks
type AdvisoryLock struct {
	db     *sql.DB
	lockID int64
}

// NewAdvisoryLock creates a new advisory lock manager
// The key is hashed to create a unique lock ID
func NewAdvisoryLock(db *sql.DB, key string) *AdvisoryLock {
	h := fnv.New64a()
	h.Write([]byte(key))
	return &AdvisoryLock{
		db:     db,
		lockID: int64(h.Sum64()),
	}
}

// Lock acquires an exclusive advisory lock
// This will block until the lock is available
func (l *AdvisoryLock) Lock(ctx context.Context) error {
	query := "SELECT pg_advisory_lock($1)"
	_, err := l.db.ExecContext(ctx, query, l.lockID)
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock %d: %w", l.lockID, err)
	}
	return nil
}

// TryLock attempts to acquire an exclusive advisory lock without blocking
// Returns true if the lock was acquired, false if it's already held
func (l *AdvisoryLock) TryLock(ctx context.Context) (bool, error) {
	var acquired bool
	query := "SELECT pg_try_advisory_lock($1)"
	err := l.db.QueryRowContext(ctx, query, l.lockID).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("failed to try advisory lock %d: %w", l.lockID, err)
	}
	return acquired, nil
}

// Unlock releases the advisory lock
func (l *AdvisoryLock) Unlock(ctx context.Context) error {
	query := "SELECT pg_advisory_unlock($1)"
	var unlocked bool
	err := l.db.QueryRowContext(ctx, query, l.lockID).Scan(&unlocked)
	if err != nil {
		return fmt.Errorf("failed to release advisory lock %d: %w", l.lockID, err)
	}
	if !unlocked {
		return fmt.Errorf("advisory lock %d was not held", l.lockID)
	}
	return nil
}

// WithLock executes a function while holding an advisory lock
func (l *AdvisoryLock) WithLock(ctx context.Context, fn func() error) error {
	if err := l.Lock(ctx); err != nil {
		return err
	}
	defer func() { _ = l.Unlock(ctx) }() // Best effort unlock

	return fn()
}

// Transaction-scoped advisory locks

// LockInTx acquires a transaction-scoped advisory lock
// The lock is automatically released when the transaction ends
func LockInTx(ctx context.Context, tx *sql.Tx, lockID int64) error {
	query := "SELECT pg_advisory_xact_lock($1)"
	_, err := tx.ExecContext(ctx, query, lockID)
	if err != nil {
		return fmt.Errorf("failed to acquire transaction advisory lock %d: %w", lockID, err)
	}
	return nil
}

// TryLockInTx attempts to acquire a transaction-scoped advisory lock without blocking
func TryLockInTx(ctx context.Context, tx *sql.Tx, lockID int64) (bool, error) {
	var acquired bool
	query := "SELECT pg_try_advisory_xact_lock($1)"
	err := tx.QueryRowContext(ctx, query, lockID).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("failed to try transaction advisory lock %d: %w", lockID, err)
	}
	return acquired, nil
}

// GenerateLockID generates a unique lock ID from a string key
func GenerateLockID(key string) int64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return int64(h.Sum64())
}
