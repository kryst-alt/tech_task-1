// Package config reads each service's settings from the environment.
// Every problem is collected, so a misconfigured deploy fails with the full
// list instead of one error per restart.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

type Google struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	TokenKey     []byte
}

// API is the lead-api service: lead intake, stage changes, Google sign-in.
type API struct {
	Port               int
	DatabaseURL        string
	LogLevel           slog.Level
	SigningSecret      []byte
	AdminAPIKey        []byte
	SignatureTolerance time.Duration
	Google             Google
}

// Sync is the sheets-sync service: copies leads to Google Sheets.
type Sync struct {
	DatabaseURL string
	LogLevel    slog.Level
	Interval    time.Duration
	BatchSize   int
	Google      Google
}

func LoadAPI(getenv func(string) string) (API, error) {
	r := reader{getenv: getenv}
	c := API{
		Port:               r.int("PORT", 8080, 1, 65535),
		DatabaseURL:        r.required("DATABASE_URL"),
		LogLevel:           r.logLevel(),
		SigningSecret:      r.secret("LEAD_SIGNING_SECRET"),
		AdminAPIKey:        r.secret("ADMIN_API_KEY"),
		SignatureTolerance: r.duration("SIGNATURE_TOLERANCE", 5*time.Minute),
	}
	c.Google = r.google(fmt.Sprintf("http://localhost:%d/auth/google/callback", c.Port))
	return c, errors.Join(r.errs...)
}

func LoadSync(getenv func(string) string) (Sync, error) {
	r := reader{getenv: getenv}
	c := Sync{
		DatabaseURL: r.required("DATABASE_URL"),
		LogLevel:    r.logLevel(),
		Interval:    r.duration("SYNC_INTERVAL", 2*time.Second),
		BatchSize:   r.int("SYNC_BATCH_SIZE", 50, 1, 500),
		// The sync service only refreshes tokens, so the redirect URL is never used.
		Google: r.google(""),
	}
	return c, errors.Join(r.errs...)
}

type reader struct {
	getenv func(string) string
	errs   []error
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *reader) required(key string) string {
	v := r.getenv(key)
	if v == "" {
		r.fail("%s is required", key)
	}
	return v
}

func (r *reader) secret(key string) []byte {
	v := r.required(key)
	if v != "" && len(v) < 32 {
		r.fail("%s must be at least 32 characters (generate with: openssl rand -hex 32)", key)
	}
	return []byte(v)
}

func (r *reader) int(key string, def, lo, hi int) int {
	v := r.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		r.fail("%s must be a number from %d to %d, got %q", key, lo, hi, v)
	}
	return n
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := r.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.fail("%s must be a positive duration such as 2s or 5m, got %q", key, v)
	}
	return d
}

func (r *reader) logLevel() slog.Level {
	var l slog.Level
	if v := r.getenv("LOG_LEVEL"); v != "" {
		if err := l.UnmarshalText([]byte(v)); err != nil {
			r.fail("LOG_LEVEL must be debug, info, warn or error, got %q", v)
		}
	}
	return l
}

func (r *reader) google(defaultRedirect string) Google {
	google := Google{
		ClientID:     r.required("GOOGLE_CLIENT_ID"),
		ClientSecret: r.required("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  r.getenv("GOOGLE_REDIRECT_URL"),
	}
	if google.RedirectURL == "" {
		google.RedirectURL = defaultRedirect
	}
	if encodedKey := r.required("TOKEN_ENCRYPTION_KEY"); encodedKey != "" {
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil || len(key) != 32 {
			r.fail("TOKEN_ENCRYPTION_KEY must be 32 bytes, base64-encoded (generate with: openssl rand -base64 32)")
		}
		google.TokenKey = key
	}
	return google
}
