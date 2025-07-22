package testdbpool

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TestDB struct {
	*pgxpool.Pool
}

func (db *TestDB) Release(ctx context.Context) error {
	return fmt.Errorf("not implemented")
}
