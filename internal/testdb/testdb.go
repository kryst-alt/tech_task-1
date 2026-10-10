// Package testdb runs one Postgres container per test binary.
//
// A package that needs a database calls Run from TestMain and Pool from each test.
package testdb

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"leadmgr/internal/store"
)

var url string

func Run(m *testing.M) int {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "docker.io/library/postgres:17-alpine",
		postgres.WithDatabase("leads"),
		postgres.WithUsername("leads"),
		postgres.WithPassword("leads"),
		postgres.BasicWaitStrategies(),
	)
	// Ryuk is off under Podman, so the container must be removed here.
	defer testcontainers.TerminateContainer(ctr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testdb: start postgres:", err)
		return 1
	}

	url, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testdb:", err)
		return 1
	}
	pool, err := store.Open(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testdb:", err)
		return 1
	}
	pool.Close()

	return m.Run()
}

// Pool returns a connection pool to a database with every table emptied.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "TRUNCATE leads, lead_sync, google_credentials CASCADE"); err != nil {
		t.Fatal(err)
	}
	return pool
}
