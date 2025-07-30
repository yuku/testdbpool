package testdbpool_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuku/testdbpool"
	"github.com/yuku/testdbpool/internal/testutil"
)

// TestIntegration_SingleSequential is an integration test that tests a single
// testdbpool instance with sequential execution.
func TestIntegration_SingleSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("acquire sequentially without resource occupation", func(t *testing.T) {
		// Create a testdbpool instance with two databases.
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "integration_single_sequential",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)
		t.Cleanup(pool.Cleanup)

		db1, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.Equal(t, "testdbpool_integration_single_sequential_0", db1.Name())
		require.Equal(t, db1.Name(), db1.Pool().Config().ConnConfig.Database)

		db2, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.Equal(t, "testdbpool_integration_single_sequential_1", db2.Name())
		require.Equal(t, db2.Name(), db2.Pool().Config().ConnConfig.Database)

		var count1 int
		err = db1.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count1)
		require.NoError(t, err)
		assert.Equal(t, 0, count1, "expected 0 rows in foos table")

		_, err = db1.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		err = db1.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count1)
		require.NoError(t, err)
		assert.Equal(t, 1, count1, "expected 1 row in foos table")

		var id1 int
		err = db1.Pool().QueryRow(ctx, `SELECT id FROM foos WHERE name = $1`, "foo1").Scan(&id1)
		require.NoError(t, err)
		assert.Equal(t, 1, id1, "expected id 1 for foo1")

		var count2 int
		err = db2.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count2)
		require.NoError(t, err)
		assert.Equal(t, 0, count2, "insert in db1 should not affect db2")

		// Release db1 so it can be reused
		require.NoError(t, db1.Release(ctx))

		// Acquire again to ensure the database is reset. Since db1 was released,
		// it should be done without blocking.
		acquireCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		t.Cleanup(cancel)
		db3, err := pool.Acquire(acquireCtx)
		require.NoError(t, err)
		require.Equal(t, "testdbpool_integration_single_sequential_0", db3.Name())
		require.Equal(t, db3.Name(), db3.Pool().Config().ConnConfig.Database)

		var count3 int
		err = db3.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count3)
		require.NoError(t, err)
		assert.Equal(t, 0, count3, "db3 should have a clean state after db1 release")

		_, err = db3.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		var id3 int
		err = db3.Pool().QueryRow(ctx, `SELECT id FROM foos WHERE name = $1`, "foo1").Scan(&id3)
		require.NoError(t, err)
		assert.Equal(t, 1, id3, "expected id 1 for foo1 in db3 after reset")
	})

	t.Run("acquire sequentially with resource occupation", func(t *testing.T) {
		// Create a testdbpool instance with two databases.
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "integration_single_sequential_occupied",
			Pool:         connPool,
			MaxDatabases: 1,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)
		t.Cleanup(pool.Cleanup)

		db1, err := pool.Acquire(ctx)
		require.NoError(t, err)

		_, err = db1.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		go func() {
			time.Sleep(50 * time.Millisecond) // Simulate some work
			require.NoError(t, db1.Release(ctx))
		}()

		acquireCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		t.Cleanup(cancel)

		db2, err := pool.Acquire(acquireCtx)
		require.NoError(t, err)

		var count2 int
		err = db2.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count2)
		require.NoError(t, err)
		assert.Equal(t, 0, count2, "insert in db1 should not affect db2")
	})
}

// TestIntegration_SingleConcurrent is an integration test that tests a single
// testdbpool instance with parallel execution.
func TestIntegration_SingleConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	pool, err := testdbpool.New(ctx, &testdbpool.Config{
		ID:           "integration_single_concurrent",
		Pool:         connPool,
		MaxDatabases: 2,
		SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
			return err
		},
	})

	require.NoError(t, err)
	require.NotNil(t, pool)
	t.Cleanup(pool.Cleanup)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)

	n := 3 // Reduce concurrency significantly for CI stability
	wg := sync.WaitGroup{}
	wg.Add(n)
	var count int32

	for range n {
		go func() {
			defer wg.Done()

			db, err := pool.Acquire(ctxWithTimeout)
			require.NoError(t, err)

			var count1 int
			err = db.Pool().QueryRow(ctxWithTimeout, `SELECT COUNT(*) FROM foos`).Scan(&count1)
			require.NoError(t, err)
			assert.Equal(t, 0, count1, "expected 0 rows in foos table")

			_, err = db.Pool().Exec(ctxWithTimeout, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
			require.NoError(t, err)

			err = db.Pool().QueryRow(ctxWithTimeout, `SELECT COUNT(*) FROM foos`).Scan(&count1)
			require.NoError(t, err)
			assert.Equal(t, 1, count1, "expected 1 row in foos table")

			require.NoError(t, db.Release(ctxWithTimeout))
			atomic.AddInt32(&count, 1)
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(n), count, "expected all goroutines to complete")
}

// TestIntegration_MultipleSequential is an integration test that tests multiple
// testdbpool instances with sequential execution.
func TestIntegration_MultipleSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("acquire sequentially without resource occupation", func(t *testing.T) {
		pools := make([]*testdbpool.Pool, 3)
		for i := range len(pools) {
			pool, err := testdbpool.New(ctx, &testdbpool.Config{
				ID:           "integration_multiple_sequential",
				Pool:         connPool,
				MaxDatabases: 2,
				SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
					_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
					return err
				},
			})
			require.NoError(t, err)
			require.NotNil(t, pool)

			t.Cleanup(func() {
				if i == 0 {
					pool.Cleanup()
				} else {
					_ = pool.Close(ctx)
				}
			})

			pools[i] = pool
		}

		db1, err := pools[0].Acquire(ctx)
		require.NoError(t, err)

		db2, err := pools[1].Acquire(ctx)
		require.NoError(t, err)

		var count1 int
		err = db1.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count1)
		require.NoError(t, err)
		assert.Equal(t, 0, count1, "expected 0 rows in foos table")

		_, err = db1.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		err = db1.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count1)
		require.NoError(t, err)
		assert.Equal(t, 1, count1, "expected 1 row in foos table")

		var id1 int
		err = db1.Pool().QueryRow(ctx, `SELECT id FROM foos WHERE name = $1`, "foo1").Scan(&id1)
		require.NoError(t, err)
		assert.Equal(t, 1, id1, "expected id 1 for foo1")

		var count2 int
		err = db2.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count2)
		require.NoError(t, err)
		assert.Equal(t, 0, count2, "insert in db1 should not affect db2")

		// Release db1 so it can be reused
		require.NoError(t, db1.Release(ctx))

		// Acquire again to ensure the database is reset. Since db1 was released,
		// it should be done without blocking.
		acquireCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		t.Cleanup(cancel)
		db3, err := pools[2].Acquire(acquireCtx)
		require.NoError(t, err)

		var count3 int
		err = db3.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count3)
		require.NoError(t, err)
		assert.Equal(t, 0, count3, "db3 should have a clean state after db1 release")

		_, err = db3.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		var id3 int
		err = db3.Pool().QueryRow(ctx, `SELECT id FROM foos WHERE name = $1`, "foo1").Scan(&id3)
		require.NoError(t, err)
		assert.Equal(t, 1, id3, "expected id 1 for foo1 in db3 after reset")
	})

	t.Run("acquire sequentially with resource occupation", func(t *testing.T) {
		pools := make([]*testdbpool.Pool, 2)
		for i := range len(pools) {
			pool, err := testdbpool.New(ctx, &testdbpool.Config{
				ID:           "integration_multiple_sequential_occupied",
				Pool:         connPool,
				MaxDatabases: 1,
				SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
					_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
					return err
				},
			})
			require.NoError(t, err)
			require.NotNil(t, pool)

			t.Cleanup(func() {
				if i == 0 {
					pool.Cleanup()
				} else {
					_ = pool.Close(ctx)
				}
			})

			pools[i] = pool
		}

		db1, err := pools[0].Acquire(ctx)
		require.NoError(t, err)

		_, err = db1.Pool().Exec(ctx, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
		require.NoError(t, err)

		go func() {
			time.Sleep(50 * time.Millisecond) // Simulate some work
			require.NoError(t, db1.Release(ctx))
		}()

		acquireCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		t.Cleanup(cancel)

		db2, err := pools[1].Acquire(acquireCtx)
		require.NoError(t, err)

		var count2 int
		err = db2.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM foos`).Scan(&count2)
		require.NoError(t, err)
		assert.Equal(t, 0, count2, "insert in db1 should not affect db2")
	})
}

// TestIntegration_MultipleConcurrent is an integration test that tests multiple
// testdbpool instances with parallel execution.
func TestIntegration_MultipleConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	pools := make([]*testdbpool.Pool, 3)
	for i := range len(pools) {
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:           "integration_multiple_concurrent",
			Pool:         connPool,
			MaxDatabases: 2,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE foos (id SERIAL PRIMARY KEY, name TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)

		t.Cleanup(func() {
			if i == 0 {
				pool.Cleanup()
			} else {
				_ = pool.Close(ctx)
			}
		})

		pools[i] = pool
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)

	n := 3 // Reduce concurrency significantly for CI stability
	wg := sync.WaitGroup{}
	wg.Add(n)
	var count int32

	for i := range n {
		go func(index int) {
			defer wg.Done()

			db, err := pools[index%len(pools)].Acquire(ctxWithTimeout)
			require.NoError(t, err)

			var count1 int
			err = db.Pool().QueryRow(ctxWithTimeout, `SELECT COUNT(*) FROM foos`).Scan(&count1)
			require.NoError(t, err)
			assert.Equal(t, 0, count1, "expected 0 rows in foos table")

			_, err = db.Pool().Exec(ctxWithTimeout, `INSERT INTO foos (name) VALUES ($1)`, "foo1")
			require.NoError(t, err)

			err = db.Pool().QueryRow(ctxWithTimeout, `SELECT COUNT(*) FROM foos`).Scan(&count1)
			require.NoError(t, err)
			assert.Equal(t, 1, count1, "expected 1 row in foos table")

			require.NoError(t, db.Release(ctxWithTimeout))
			atomic.AddInt32(&count, 1)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(n), count, "expected all goroutines to complete")
}

// TestIntegration_DatabaseOwner is an integration test that tests the DatabaseOwner functionality.
func TestIntegration_DatabaseOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	connPool := testutil.GetTestDBPool(t)
	t.Cleanup(testutil.CleanupNumpool(connPool))

	t.Run("with valid database owner", func(t *testing.T) {
		// First, create a test user that will own the databases
		testOwner := "testowner"
		_, err := connPool.Exec(ctx, `DROP USER IF EXISTS `+testOwner)
		require.NoError(t, err)

		_, err = connPool.Exec(ctx, `CREATE USER `+testOwner+` WITH LOGIN`)
		require.NoError(t, err)

		t.Cleanup(func() {
			// Clean up the test user after the test
			_, _ = connPool.Exec(ctx, `DROP USER IF EXISTS `+testOwner)
		})

		// Create a testdbpool instance with DatabaseOwner
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:            "integration_database_owner",
			Pool:          connPool,
			MaxDatabases:  1,
			DatabaseOwner: testOwner,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, data TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)
		t.Cleanup(pool.Cleanup)

		// Acquire a database
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)

		// Verify the database owner by checking pg_database
		var owner string
		err = connPool.QueryRow(ctx,
			`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`,
			db.Name()).Scan(&owner)
		require.NoError(t, err)
		assert.Equal(t, testOwner, owner, "database should be owned by the specified owner")

		// Verify the template database owner as well
		var templateOwner string
		err = connPool.QueryRow(ctx,
			`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`,
			pool.TemplateDBName()).Scan(&templateOwner)
		require.NoError(t, err)
		assert.Equal(t, testOwner, templateOwner, "template database should be owned by the specified owner")

		// Verify that the database functions normally
		_, err = db.Pool().Exec(ctx, `INSERT INTO test_table (data) VALUES ($1)`, "test data")
		require.NoError(t, err)

		var count int
		err = db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM test_table`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "should be able to insert and query data")

		require.NoError(t, db.Release(ctx))
	})

	t.Run("without database owner (default behavior)", func(t *testing.T) {
		// Create a testdbpool instance without DatabaseOwner (empty string)
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:            "integration_no_database_owner",
			Pool:          connPool,
			MaxDatabases:  1,
			DatabaseOwner: "", // Empty string should use default behavior
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, data TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)
		t.Cleanup(pool.Cleanup)

		// Acquire a database
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)

		// Verify that the database functions normally with default owner
		_, err = db.Pool().Exec(ctx, `INSERT INTO test_table (data) VALUES ($1)`, "test data")
		require.NoError(t, err)

		var count int
		err = db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM test_table`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "should be able to insert and query data with default owner")

		require.NoError(t, db.Release(ctx))
	})

	t.Run("with underscore in database owner name", func(t *testing.T) {
		// Test with a valid PostgreSQL identifier that contains underscores
		testOwner := "test_owner_123"
		_, err := connPool.Exec(ctx, `DROP USER IF EXISTS `+testOwner)
		require.NoError(t, err)

		_, err = connPool.Exec(ctx, `CREATE USER `+testOwner+` WITH LOGIN`)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = connPool.Exec(ctx, `DROP USER IF EXISTS `+testOwner)
		})

		// Create a testdbpool instance with underscore in DatabaseOwner
		pool, err := testdbpool.New(ctx, &testdbpool.Config{
			ID:            "integration_database_owner_underscore",
			Pool:          connPool,
			MaxDatabases:  1,
			DatabaseOwner: testOwner,
			SetupTemplate: func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, `CREATE TABLE test_table (id SERIAL PRIMARY KEY, data TEXT)`)
				return err
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pool)
		t.Cleanup(pool.Cleanup)

		// Acquire a database
		db, err := pool.Acquire(ctx)
		require.NoError(t, err)

		// Verify the database owner
		var owner string
		err = connPool.QueryRow(ctx,
			`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`,
			db.Name()).Scan(&owner)
		require.NoError(t, err)
		assert.Equal(t, testOwner, owner, "database should be owned by the specified owner with underscores")

		require.NoError(t, db.Release(ctx))
	})
}
