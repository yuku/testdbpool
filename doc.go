// Package testdbpool provides a pool of test databases for PostgreSQL.
//
// testdbpool manages a pool of PostgreSQL databases specifically designed for testing.
// It creates databases from a template for fast initialization and automatically
// resets them between uses. The pool is built on top of github.com/yuku/numpool
// for efficient resource management and concurrent access.
//
// Key features:
//   - Template-based database creation for fast setup
//   - Automatic database reset between test runs
//   - Concurrent access with fair queuing
//   - Process-based cleanup (databases are released when processes die)
//   - Configurable pool size (1-64 databases)
//
// Basic usage:
//
//	config := &testdbpool.Config{
//	    PoolID:       "myapp-test",
//	    DBPool:       pgxPool,
//	    MaxDatabases: 5,
//	    SetupTemplate: setupFunc,
//	    ResetDatabase: resetFunc,
//	}
//
//	pool, err := testdbpool.New(ctx, config)
//	if err != nil {
//	    // handle error
//	}
//
//	db, err := pool.Acquire(ctx)
//	if err != nil {
//	    // handle error
//	}
//	defer db.Close()
//
//	// Use db.Conn() to access the *pgx.Conn
package testdbpool
