package sheets_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/lead"
	"leadmgr/internal/sheets"
	"leadmgr/internal/store"
	"leadmgr/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }

type fakeSheet struct {
	rows  map[string][]any
	fail  error
	calls int
}

func (f *fakeSheet) UpsertRow(_ context.Context, row []any) error {
	f.calls++
	if f.fail != nil {
		return f.fail
	}
	f.rows[row[0].(string)] = row
	return nil
}

func setup(t *testing.T) (*pgxpool.Pool, *fakeSheet, *sheets.Worker) {
	pool := testdb.Pool(t)
	fake := &fakeSheet{rows: map[string][]any{}}
	w := &sheets.Worker{Pool: pool, SheetFor: func(context.Context) (sheets.Sheet, error) { return fake, nil }}
	return pool, fake, w
}

func saveLead(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	in := lead.Incoming{ExternalID: id, Fields: lead.Fields{Name: name, Email: id + "@example.com", Company: "Acme", Source: "web"}}
	if _, err := store.UpsertLead(context.Background(), pool, in); err != nil {
		t.Fatal(err)
	}
}

func syncDue(t *testing.T, w *sheets.Worker) int {
	t.Helper()
	n, err := w.SyncDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPendingLeadIsWrittenOnce(t *testing.T) {
	pool, fake, w := setup(t)
	saveLead(t, pool, "p-1", "Ada")

	if n := syncDue(t, w); n != 1 {
		t.Fatalf("first pass synced %d, want 1", n)
	}
	row := fake.rows["p-1"]
	if len(row) != 8 || row[1] != "Ada" || row[2] != "p-1@example.com" || row[5] != "new" {
		t.Fatalf("row = %v, want [p-1 Ada p-1@example.com Acme web new <created> <updated>]", row)
	}
	if n := syncDue(t, w); n != 0 || fake.calls != 1 {
		t.Fatalf("second pass synced %d with %d sheet calls, want 0 and 1", n, fake.calls)
	}
}

func TestChangedLeadIsWrittenAgain(t *testing.T) {
	pool, fake, w := setup(t)
	saveLead(t, pool, "p-1", "Ada")
	syncDue(t, w)

	saveLead(t, pool, "p-1", "Ada Lovelace")
	if err := store.ChangeStage(context.Background(), pool, "p-1", lead.StageQualified); err != nil {
		t.Fatal(err)
	}
	if n := syncDue(t, w); n != 1 {
		t.Fatalf("synced %d, want 1 (two changes coalesce into one write)", n)
	}
	if row := fake.rows["p-1"]; row[1] != "Ada Lovelace" || row[5] != "qualified" {
		t.Fatalf("row = %v, want latest name and stage", row)
	}
}

func TestFailedSyncBacksOffThenRecovers(t *testing.T) {
	pool, fake, w := setup(t)
	saveLead(t, pool, "p-1", "Ada")
	fake.fail = errors.New("sheets api: 503")

	if n := syncDue(t, w); n != 0 {
		t.Fatalf("synced %d, want 0", n)
	}
	var attempts int
	var lastError string
	var due bool
	pool.QueryRow(context.Background(),
		"SELECT attempts, last_error, next_attempt_at <= now() FROM lead_sync").Scan(&attempts, &lastError, &due)
	if attempts != 1 || lastError != "sheets api: 503" || due {
		t.Fatalf("attempts=%d last_error=%q due=%v, want 1, the error, not due", attempts, lastError, due)
	}

	fake.fail = nil
	if n := syncDue(t, w); n != 0 || fake.calls != 1 {
		t.Fatalf("retried before backoff elapsed: synced %d, calls %d", n, fake.calls)
	}
	pool.Exec(context.Background(), "UPDATE lead_sync SET next_attempt_at = now()")
	if n := syncDue(t, w); n != 1 || fake.rows["p-1"] == nil {
		t.Fatalf("synced %d after backoff, want 1", n)
	}
}

func TestNothingHappensUntilGoogleIsLinked(t *testing.T) {
	pool, _, w := setup(t)
	w.SheetFor = func(context.Context) (sheets.Sheet, error) { return nil, sheets.ErrNotLinked }
	saveLead(t, pool, "p-1", "Ada")

	if n := syncDue(t, w); n != 0 {
		t.Fatalf("synced %d, want 0", n)
	}
	var attempts int
	pool.QueryRow(context.Background(), "SELECT attempts FROM lead_sync").Scan(&attempts)
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (not linked is not a failure)", attempts)
	}
}

func TestRevokedTokenFlagsReauthWithoutCountingFailure(t *testing.T) {
	pool, fake, w := setup(t)
	pool.Exec(context.Background(),
		"INSERT INTO google_credentials (account_email, refresh_token_enc) VALUES ('me@example.com', '\\x00')")
	saveLead(t, pool, "p-1", "Ada")
	fake.fail = sheets.ErrReauthRequired

	if n := syncDue(t, w); n != 0 {
		t.Fatalf("synced %d, want 0", n)
	}
	var needsReauth bool
	var attempts int
	pool.QueryRow(context.Background(), "SELECT needs_reauth FROM google_credentials").Scan(&needsReauth)
	pool.QueryRow(context.Background(), "SELECT attempts FROM lead_sync").Scan(&attempts)
	if !needsReauth || attempts != 0 {
		t.Fatalf("needs_reauth=%v attempts=%d, want true 0", needsReauth, attempts)
	}
}

func TestBackoffStaysCappedAfterManyFailures(t *testing.T) {
	pool, fake, w := setup(t)
	saveLead(t, pool, "p-1", "Ada")
	pool.Exec(context.Background(), "UPDATE lead_sync SET attempts = 60")
	fake.fail = errors.New("sheets api: 503")

	if _, err := w.SyncDue(context.Background()); err != nil {
		t.Fatalf("SyncDue after 60 failures: %v", err)
	}
	var waitMinutes float64
	pool.QueryRow(context.Background(),
		"SELECT extract(epoch FROM next_attempt_at - now()) / 60 FROM lead_sync").Scan(&waitMinutes)
	if waitMinutes < 14 || waitMinutes > 15.1 {
		t.Fatalf("next attempt in %.1f min, want 15", waitMinutes)
	}
}

type blockingSheet struct{ started, release chan struct{} }

func (b blockingSheet) UpsertRow(context.Context, []any) error {
	close(b.started)
	<-b.release
	return nil
}

func TestSlowSheetDoesNotBlockLeadIntake(t *testing.T) {
	pool, _, w := setup(t)
	saveLead(t, pool, "p-1", "Ada")
	b := blockingSheet{started: make(chan struct{}), release: make(chan struct{})}
	w.SheetFor = func(context.Context) (sheets.Sheet, error) { return b, nil }

	done := make(chan struct{})
	go func() { w.SyncDue(context.Background()); close(done) }()
	<-b.started
	defer func() { close(b.release); <-done }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	in := lead.Incoming{ExternalID: "p-1", Fields: lead.Fields{Name: "Ada L", Email: "p-1@example.com", Company: "Acme", Source: "web"}}
	if _, err := store.UpsertLead(ctx, pool, in); err != nil {
		t.Fatalf("lead intake blocked while Google was slow: %v", err)
	}
}

func TestRevokedTokenDuringSetupFlagsReauth(t *testing.T) {
	pool, _, w := setup(t)
	pool.Exec(context.Background(),
		"INSERT INTO google_credentials (account_email, refresh_token_enc) VALUES ('me@example.com', '\\x00')")
	w.SheetFor = func(context.Context) (sheets.Sheet, error) { return nil, sheets.ErrReauthRequired }

	syncDue(t, w)
	var needsReauth bool
	pool.QueryRow(context.Background(), "SELECT needs_reauth FROM google_credentials").Scan(&needsReauth)
	if !needsReauth {
		t.Fatal("needs_reauth = false, want true")
	}
}

func TestLinkingDifferentAccountStartsFreshSheet(t *testing.T) {
	pool, _, w := setup(t)
	ctx := context.Background()
	q := store.New(pool)
	saveLead(t, pool, "p-1", "Ada")
	syncDue(t, w)
	q.SaveGoogleCredentials(ctx, store.SaveGoogleCredentialsParams{AccountEmail: "me@example.com", RefreshTokenEnc: []byte{1}})
	q.SetSpreadsheetID(ctx, "sheet-of-me")

	q.SaveGoogleCredentials(ctx, store.SaveGoogleCredentialsParams{AccountEmail: "me@example.com", RefreshTokenEnc: []byte{2}})
	var sheetID *string
	var pending int
	pool.QueryRow(ctx, "SELECT spreadsheet_id FROM google_credentials").Scan(&sheetID)
	pool.QueryRow(ctx, "SELECT count(*) FROM lead_sync s JOIN leads l ON l.id = s.lead_id WHERE l.version > s.synced_version").Scan(&pending)
	if sheetID == nil || *sheetID != "sheet-of-me" || pending != 0 {
		t.Fatalf("same account relink: sheet=%v pending=%d, want sheet-of-me and 0", sheetID, pending)
	}

	q.SaveGoogleCredentials(ctx, store.SaveGoogleCredentialsParams{AccountEmail: "other@example.com", RefreshTokenEnc: []byte{3}})
	pool.QueryRow(ctx, "SELECT spreadsheet_id FROM google_credentials").Scan(&sheetID)
	pool.QueryRow(ctx, "SELECT count(*) FROM lead_sync s JOIN leads l ON l.id = s.lead_id WHERE l.version > s.synced_version").Scan(&pending)
	if sheetID != nil || pending != 1 {
		t.Fatalf("new account: sheet=%v pending=%d, want nil and 1 (new sheet gets every lead)", sheetID, pending)
	}
}
