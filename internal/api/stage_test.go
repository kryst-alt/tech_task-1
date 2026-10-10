package api

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"leadmgr/internal/testdb"
)

const testAdminKey = "admin-key-admin-key-admin-key-admin-key"

func patchStage(t *testing.T, app *fiber.App, leadID, body, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("PATCH", "/v1/leads/"+leadID+"/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestStageMovesAlongAllowedPath(t *testing.T) {
	app, pool := newTestApp(t)
	postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))

	status, resp := patchStage(t, app, "p-1", `{"stage":"qualified"}`, testAdminKey)
	if status != 200 || resp != `{"stage":"qualified"}` {
		t.Fatalf("got %d %s, want 200 {\"stage\":\"qualified\"}", status, resp)
	}
	var stage string
	var version, pending int
	pool.QueryRow(context.Background(), "SELECT stage, version FROM leads WHERE external_id = 'p-1'").Scan(&stage, &version)
	pool.QueryRow(context.Background(),
		"SELECT count(*) FROM leads l JOIN lead_sync s ON s.lead_id = l.id WHERE l.version > s.synced_version").Scan(&pending)
	if stage != "qualified" || version != 2 || pending != 1 {
		t.Fatalf("stage=%q version=%d pending=%d, want qualified 2 1", stage, version, pending)
	}
}

func TestStageRejectsSkippingAhead(t *testing.T) {
	app, pool := newTestApp(t)
	postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))

	if status, resp := patchStage(t, app, "p-1", `{"stage":"won"}`, testAdminKey); status != 409 {
		t.Fatalf("got %d %s, want 409", status, resp)
	}
	var stage string
	pool.QueryRow(context.Background(), "SELECT stage FROM leads WHERE external_id = 'p-1'").Scan(&stage)
	if stage != "new" {
		t.Fatalf("stage = %q, want new", stage)
	}
}

func TestStageRequestErrors(t *testing.T) {
	cases := []struct {
		name, leadID, body, token string
		want                      int
	}{
		{"no token", "p-1", `{"stage":"qualified"}`, "", 401},
		{"wrong token", "p-1", `{"stage":"qualified"}`, "wrong-key-wrong-key-wrong-key-wrong", 401},
		{"unknown lead", "p-404", `{"stage":"qualified"}`, testAdminKey, 404},
		{"unknown stage", "p-1", `{"stage":"maybe"}`, testAdminKey, 422},
		{"not json", "p-1", `{`, testAdminKey, 422},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app, pool := newTestApp(t)
			postSigned(t, app, leadJSON("p-1", "Ada", "ada@example.com"))
			if status, resp := patchStage(t, app, c.leadID, c.body, c.token); status != c.want {
				t.Fatalf("got %d %s, want %d", status, resp, c.want)
			}
			var stage string
			pool.QueryRow(context.Background(), "SELECT stage FROM leads WHERE external_id = 'p-1'").Scan(&stage)
			if stage != "new" {
				t.Fatalf("stage = %q, want unchanged new", stage)
			}
		})
	}
}

func TestStageRejectsEmptyTokenWhenNoAdminKeyConfigured(t *testing.T) {
	pool := testdb.Pool(t)
	app := New(Deps{Pool: pool, SigningSecret: testSecret})
	req := httptest.NewRequest("PATCH", "/v1/leads/p-1/stage", strings.NewReader(`{"stage":"qualified"}`))
	req.Header.Set("Authorization", "Bearer ")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
