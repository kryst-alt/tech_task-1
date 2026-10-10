// lead-api receives signed leads from the partner, changes deal stages, and
// runs the Google sign-in that sheets-sync depends on.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	"leadmgr/internal/config"
	"leadmgr/internal/sheets"
	"leadmgr/internal/store"
	"leadmgr/services/lead-api/api"
)

func main() {
	if err := run(); err != nil {
		slog.Error("lead-api stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAPI(os.Getenv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	slog.SetLogLoggerLevel(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	app := api.New(api.Deps{
		Pool:               pool,
		SigningSecret:      cfg.SigningSecret,
		AdminAPIKey:        cfg.AdminAPIKey,
		SignatureTolerance: cfg.SignatureTolerance,
		Google: &sheets.Google{
			Pool:  pool,
			OAuth: sheets.OAuthConfig(cfg.Google.ClientID, cfg.Google.ClientSecret, cfg.Google.RedirectURL),
			Key:   cfg.Google.TokenKey,
		},
	})

	errc := make(chan error, 1)
	go func() {
		errc <- app.Listen(fmt.Sprintf(":%d", cfg.Port), fiber.ListenConfig{DisableStartupMessage: true})
	}()
	slog.Info("lead-api listening", "port", cfg.Port, "google_redirect", cfg.Google.RedirectURL)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.ShutdownWithContext(shutdownCtx)
}
