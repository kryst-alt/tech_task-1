package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
)

type Config struct {
	Port               int
	DatabaseURL        string
	SigningSecret      []byte
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	TokenKey           []byte
}

// Load reads every setting at once and reports all problems together, so a
// misconfigured deploy fails with the full list instead of one error per restart.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	required := func(key string) string {
		v := getenv(key)
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}

	c := Config{
		DatabaseURL:        required("DATABASE_URL"),
		SigningSecret:      []byte(required("LEAD_SIGNING_SECRET")),
		GoogleClientID:     required("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: required("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  required("GOOGLE_REDIRECT_URL"),
		Port:               8080,
	}

	if p := getenv("PORT"); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			errs = append(errs, fmt.Errorf("PORT must be 1-65535, got %q", p))
		}
		c.Port = port
	}

	if len(c.SigningSecret) > 0 && len(c.SigningSecret) < 32 {
		errs = append(errs, errors.New("LEAD_SIGNING_SECRET must be at least 32 characters"))
	}

	if k := required("TOKEN_ENCRYPTION_KEY"); k != "" {
		key, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(key) != 32 {
			errs = append(errs, errors.New("TOKEN_ENCRYPTION_KEY must be 32 bytes, base64-encoded"))
		}
		c.TokenKey = key
	}

	return c, errors.Join(errs...)
}
