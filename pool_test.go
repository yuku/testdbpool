package testdbpool

import (
	"context"
	"testing"
)

func TestAcquire(t *testing.T) {
	dbpool, err := New(Config{
		Conn: getRootConnection(t),
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	pool, err := dbpool.Acquire()
	if err != nil {
		t.Fatalf("failed to acquire pool: %v", err)
	}

	if pool == nil {
		t.Fatal("acquired pool is nil")
	}

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping acquired pool: %v", err)
	}
}
