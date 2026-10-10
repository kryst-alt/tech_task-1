package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func env(overrides map[string]string) func(string) string {
	base := map[string]string{
		"DATABASE_URL":         "postgres://x",
		"LEAD_SIGNING_SECRET":  strings.Repeat("s", 32),
		"ADMIN_API_KEY":        strings.Repeat("a", 32),
		"GOOGLE_CLIENT_ID":     "id",
		"GOOGLE_CLIENT_SECRET": "secret",
		"TOKEN_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return func(k string) string { return base[k] }
}

func TestAPIDefaults(t *testing.T) {
	c, err := LoadAPI(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 || c.SignatureTolerance != 5*time.Minute ||
		c.Google.RedirectURL != "http://localhost:8080/auth/google/callback" {
		t.Fatalf("defaults = port %d, tolerance %s, redirect %q", c.Port, c.SignatureTolerance, c.Google.RedirectURL)
	}
}

func TestAPIRedirectFollowsPort(t *testing.T) {
	c, err := LoadAPI(env(map[string]string{"PORT": "9090"}))
	if err != nil || c.Google.RedirectURL != "http://localhost:9090/auth/google/callback" {
		t.Fatalf("redirect = %q, %v", c.Google.RedirectURL, err)
	}
}

func TestSyncNeedsNoAPISecrets(t *testing.T) {
	c, err := LoadSync(env(map[string]string{"LEAD_SIGNING_SECRET": "", "ADMIN_API_KEY": "", "SYNC_INTERVAL": "500ms", "SYNC_BATCH_SIZE": "10"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval != 500*time.Millisecond || c.BatchSize != 10 {
		t.Fatalf("interval %s batch %d, want 500ms 10", c.Interval, c.BatchSize)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	_, err := LoadAPI(env(map[string]string{
		"DATABASE_URL": "", "ADMIN_API_KEY": "short", "PORT": "99999",
		"SIGNATURE_TOLERANCE": "soon", "TOKEN_ENCRYPTION_KEY": "bm90MzJieXRlcw==",
	}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"DATABASE_URL", "ADMIN_API_KEY", "PORT", "SIGNATURE_TOLERANCE", "TOKEN_ENCRYPTION_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}
