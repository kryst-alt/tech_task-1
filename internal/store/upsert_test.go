package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"leadmgr/internal/lead"
	"leadmgr/internal/store"
	"leadmgr/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }

// Another request inserts the same new lead and commits while ours is waiting
// on the unique index. Ours must end as an update of that row, not an error.
func TestUpsertLosingInsertRaceBecomesUpdate(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	in := lead.Incoming{ExternalID: "p-1", Fields: lead.Fields{Name: "Ada", Email: "ada@example.com", Company: "Acme", Source: "web"}}

	rival, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rival.Exec(ctx,
		"INSERT INTO leads (external_id, email, name, company, source) VALUES ('p-1', 'ada@example.com', 'Ada', 'Acme', 'web')"); err != nil {
		t.Fatal(err)
	}

	type result struct {
		action lead.Action
		err    error
	}
	done := make(chan result, 1)
	go func() {
		a, err := store.UpsertLead(ctx, pool, in)
		done <- result{a, err}
	}()

	time.Sleep(300 * time.Millisecond) // let UpsertLead block on the rival's uncommitted row
	if err := rival.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	r := <-done
	if r.err != nil || r.action != lead.Unchanged {
		t.Fatalf("UpsertLead = %v, %v; want unchanged, nil", r.action, r.err)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM leads").Scan(&n)
	if n != 1 {
		t.Fatalf("leads = %d, want 1", n)
	}
}
