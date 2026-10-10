package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"leadmgr/internal/sheets"
	"leadmgr/internal/testdb"
)

func newAuthTestApp(t *testing.T) *Deps {
	pool := testdb.Pool(t)
	return &Deps{
		Pool: pool, SigningSecret: testSecret, AdminAPIKey: []byte(testAdminKey), Now: func() time.Time { return testNow },
		Google: &sheets.Google{Pool: pool, OAuth: sheets.OAuthConfig("client-id", "client-secret", "http://localhost:8080/auth/google/callback")},
	}
}

func TestGoogleSignInNeedsAdminKey(t *testing.T) {
	app := New(*newAuthTestApp(t))
	for _, target := range []string{"/auth/google", "/auth/google?key=wrong"} {
		resp, err := app.Test(httptest.NewRequest("GET", target, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 401 {
			t.Fatalf("%s: status = %d, want 401", target, resp.StatusCode)
		}
	}
}

func TestGoogleSignInRedirectsForOfflineAccess(t *testing.T) {
	app := New(*newAuthTestApp(t))
	resp, err := app.Test(httptest.NewRequest("GET", "/auth/google?key="+testAdminKey, nil))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	q := loc.Query()
	if resp.StatusCode != 302 || loc.Host != "accounts.google.com" ||
		q.Get("access_type") != "offline" || q.Get("prompt") != "consent" || q.Get("client_id") != "client-id" {
		t.Fatalf("got %d %s, want 302 to Google with offline access and forced consent", resp.StatusCode, loc)
	}
	if !strings.Contains(q.Get("scope"), "drive.file") {
		t.Fatalf("scope = %q, want drive.file", q.Get("scope"))
	}
	cookie := resp.Header.Get("Set-Cookie")
	if q.Get("state") == "" || !strings.Contains(cookie, "oauth_state="+q.Get("state")) || !strings.Contains(strings.ToLower(cookie), "httponly") {
		t.Fatalf("state %q not bound to an HttpOnly cookie: %s", q.Get("state"), cookie)
	}
}

func TestGoogleCallbackRejectsMismatchedState(t *testing.T) {
	app := New(*newAuthTestApp(t))
	req := httptest.NewRequest("GET", "/auth/google/callback?state=attacker&code=abc", nil)
	req.Header.Set("Cookie", "oauth_state=mine")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHealthReportsGoogleNotLinked(t *testing.T) {
	app := New(*newAuthTestApp(t))
	resp, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 128)
	n, _ := resp.Body.Read(body)
	if got := string(body[:n]); got != `{"database":"ok","google":"not_linked"}` {
		t.Fatalf("healthz = %s", got)
	}
}
