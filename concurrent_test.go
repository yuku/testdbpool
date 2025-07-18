package testdbpool_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testhelper"
)

func TestPoolConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool := testhelper.GetTestDBPool(t)

	config := &testdbpool.Config{
		PoolID:       "test-pool-concurrent",
		DBPool:       pool,
		MaxDatabases: 3,
		SetupTemplate: func(ctx context.Context, pool *pgxpool.Pool) error {
			_, err := pool.Exec(ctx, `CREATE TABLE concurrent_test (id SERIAL PRIMARY KEY, worker_id INT)`)
			return err
		},
		ResetDatabase: func(ctx context.Context, pool *pgxpool.Pool) error {
			_, err := pool.Exec(ctx, `TRUNCATE concurrent_test RESTART IDENTITY`)
			return err
		},
	}

	testPool, err := testdbpool.New(ctx, config)
	require.NoError(t, err, "failed to create pool")

	// Test concurrent acquisition
	t.Run("ConcurrentAcquire", func(t *testing.T) {
		const numWorkers = 10
		var wg sync.WaitGroup
		errors := make(chan error, numWorkers)

		for i := range numWorkers {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				db, err := testPool.Acquire(ctx)
				if err != nil {
					errors <- err
					return
				}
				defer func() { _ = db.Close() }()

				// Do some work
				_, err = db.Pool().Exec(ctx, "INSERT INTO concurrent_test (worker_id) VALUES ($1)", workerID)
				if err != nil {
					errors <- err
					return
				}

				// Simulate work
				time.Sleep(10 * time.Millisecond)
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("worker error: %v", err)
		}
	})

	// Test blocking when pool is exhausted
	t.Run("BlockingWhenExhausted", func(t *testing.T) {
		// Acquire all databases
		var dbs []*testdbpool.TestDB
		for i := range config.MaxDatabases {
			db, err := testPool.Acquire(ctx)
			require.NoErrorf(t, err, "failed to acquire database %d", i)
			dbs = append(dbs, db)
		}

		// Try to acquire one more with timeout
		ctxTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		done := make(chan struct{})
		var acquireErr error

		go func() {
			_, acquireErr = testPool.Acquire(ctxTimeout)
			close(done)
		}()

		select {
		case <-done:
			if acquireErr == nil {
				t.Error("expected timeout error, got nil")
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("acquire did not timeout as expected")
		}

		// Release one database
		_ = dbs[0].Release(ctx)
		dbs = dbs[1:]

		// Now acquire should succeed
		db, err := testPool.Acquire(ctx)
		require.NoError(t, err, "failed to acquire after release")
		defer func() { _ = db.Close() }()

		// Clean up
		for _, db := range dbs {
			_ = db.Release(ctx)
		}
	})
}
