package api

import (
	"io"
	"net/http/httptest"
	"os"
	"testing"

	"leadmgr/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }

func TestHealthz(t *testing.T) {
	app := New(Deps{Pool: testdb.Pool(t)})

	resp, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != `{"database":"ok"}` {
		t.Fatalf("got %d %s", resp.StatusCode, body)
	}
}
