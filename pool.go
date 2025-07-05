package testdbpool

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	rootConn *pgx.Conn
}

func (p *Pool) Acquire() (*pgxpool.Pool, error) {
	return nil, fmt.Errorf("not implemented")
}
