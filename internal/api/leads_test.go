package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/testdb"
)

var (
	testSecret = []byte("0123456789abcdef0123456789abcdef")
	testNow    = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
)

func newTestApp(t *testing.T) (*fiber.App, *pgxpool.Pool) {
	pool := testdb.Pool(t)
	app := New(Deps{Pool: pool, SigningSecret: testSecret, AdminAPIKey: []byte(testAdminKey), Now: func() time.Time { return testNow }})
	return app, pool
}

// sign builds the signature the partner sends, independently of the server code.
func sign(secret []byte, ts, body string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts + "." + body))
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func postLead(t *testing.T, app *fiber.App, body string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/leads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func postSigned(t *testing.T, app *fiber.App, body string) (int, string) {
	ts := strconv.FormatInt(testNow.Unix(), 10)
	return postLead(t, app, body, map[string]string{"X-Timestamp": ts, "X-Signature": sign(testSecret, ts, body)})
}

func leadJSON(id, name, email string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"email":%q,"company":"Acme","source":"partner-site"}`, id, name, email)
}

func countLeads(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM leads").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// R4.1
func TestLeadWithInvalidSignatureIsRejected(t *testing.T) {
	body := leadJSON("p-1", "Ada", "ada@example.com")
	ts := strconv.FormatInt(testNow.Unix(), 10)
	stale := strconv.FormatInt(testNow.Add(-6*time.Minute).Unix(), 10)

	cases := map[string]map[string]string{
		"no headers":      {},
		"missing sig":     {"X-Timestamp": ts},
		"wrong secret":    {"X-Timestamp": ts, "X-Signature": sign([]byte("not-the-secret-not-the-secret-!!"), ts, body)},
		"tampered body":   {"X-Timestamp": ts, "X-Signature": sign(testSecret, ts, body+" ")},
		"stale timestamp": {"X-Timestamp": stale, "X-Signature": sign(testSecret, stale, body)},
		"garbage sig":     {"X-Timestamp": ts, "X-Signature": "sha256=zz"},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			app, pool := newTestApp(t)
			if status, resp := postLead(t, app, body, headers); status != 401 {
				t.Fatalf("status = %d %s, want 401", status, resp)
			}
			if n := countLeads(t, pool); n != 0 {
				t.Fatalf("leads = %d, want 0", n)
			}
		})
	}
}

// R4.2
func TestDuplicateSubmissionCreatesOneRecord(t *testing.T) {
	app, pool := newTestApp(t)
	body := leadJSON("p-1", "Ada", "ada@example.com")

	if status, resp := postSigned(t, app, body); status != 201 {
		t.Fatalf("first: %d %s, want 201", status, resp)
	}
	if status, resp := postSigned(t, app, body); status != 200 {
		t.Fatalf("second: %d %s, want 200", status, resp)
	}
	if n := countLeads(t, pool); n != 1 {
		t.Fatalf("leads = %d, want 1", n)
	}
	var version int
	pool.QueryRow(context.Background(), "SELECT version FROM leads").Scan(&version)
	if version != 1 {
		t.Fatalf("version = %d, want 1 (identical resend must not count as a change)", version)
	}
}

// R4.2, with the duplicates arriving at the same moment.
func TestConcurrentDuplicatesCreateOneRecord(t *testing.T) {
	app, pool := newTestApp(t)
	body := leadJSON("p-1", "Ada", "ada@example.com")

	var wg sync.WaitGroup
	statuses := make([]int, 8)
	for i := range statuses {
		wg.Go(func() { statuses[i], _ = postSigned(t, app, body) })
	}
	wg.Wait()

	created := 0
	for _, s := range statuses {
		switch s {
		case 201:
			created++
		case 200:
		default:
			t.Fatalf("statuses = %v, want only 201/200", statuses)
		}
	}
	if created != 1 || countLeads(t, pool) != 1 {
		t.Fatalf("statuses = %v, leads = %d, want exactly one create", statuses, countLeads(t, pool))
	}
}

// R4.3
func TestLeadWithExistingEmailUpdatesRecord(t *testing.T) {
	app, pool := newTestApp(t)
	postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))

	status, resp := postSigned(t, app, leadJSON("p-2", "Ada Lovelace", "ADA@example.com"))
	if status != 200 {
		t.Fatalf("status = %d %s, want 200", status, resp)
	}
	if n := countLeads(t, pool); n != 1 {
		t.Fatalf("leads = %d, want 1", n)
	}
	var name, externalID string
	var version int
	pool.QueryRow(context.Background(), "SELECT name, external_id, version FROM leads").Scan(&name, &externalID, &version)
	if name != "Ada Lovelace" || externalID != "p-1" || version != 2 {
		t.Fatalf("got name=%q external_id=%q version=%d, want Ada Lovelace, p-1, 2", name, externalID, version)
	}
}

func TestIDAndEmailOfDifferentLeadsIsConflict(t *testing.T) {
	app, pool := newTestApp(t)
	postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))
	postSigned(t, app, leadJSON("p-2", "Grace", "grace@example.com"))

	if status, resp := postSigned(t, app, leadJSON("p-1", "Ada", "grace@example.com")); status != 409 {
		t.Fatalf("status = %d %s, want 409", status, resp)
	}
	var email string
	pool.QueryRow(context.Background(), "SELECT email FROM leads WHERE external_id = 'p-1'").Scan(&email)
	if email != "ada@example.com" {
		t.Fatalf("p-1 email = %q, want unchanged ada@example.com", email)
	}
}

func TestInvalidLeadIsRejected(t *testing.T) {
	cases := map[string]string{
		"not json":      `{`,
		"missing email": `{"id":"p-1","name":"Ada","company":"Acme","source":"x"}`,
		"bad email":     leadJSON("p-1", "Ada", "not-an-email"),
		"blank name":    leadJSON("p-1", "  ", "ada@example.com"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			app, pool := newTestApp(t)
			if status, resp := postSigned(t, app, body); status != 422 {
				t.Fatalf("status = %d %s, want 422", status, resp)
			}
			if n := countLeads(t, pool); n != 0 {
				t.Fatalf("leads = %d, want 0", n)
			}
		})
	}
}

func TestSavedLeadIsQueuedForSheetsSync(t *testing.T) {
	app, pool := newTestApp(t)
	postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))

	var pending int
	pool.QueryRow(context.Background(),
		"SELECT count(*) FROM leads l JOIN lead_sync s ON s.lead_id = l.id WHERE l.version > s.synced_version").Scan(&pending)
	if pending != 1 {
		t.Fatalf("leads pending sync = %d, want 1", pending)
	}
}
