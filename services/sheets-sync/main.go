// sheets-sync copies new and changed leads to the linked Google Sheet.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"leadmgr/internal/config"
	"leadmgr/internal/sheets"
	"leadmgr/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("sheets-sync stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadSync(os.Getenv)
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

	google := &sheets.Google{
		Pool:  pool,
		OAuth: sheets.OAuthConfig(cfg.Google.ClientID, cfg.Google.ClientSecret, cfg.Google.RedirectURL),
		Key:   cfg.Google.TokenKey,
	}
	worker := &sheets.Worker{Pool: pool, SheetFor: google.Sheet, Interval: cfg.Interval, BatchSize: int32(cfg.BatchSize)}
	slog.Info("sheets-sync running", "interval", cfg.Interval, "batch_size", cfg.BatchSize)
	worker.Run(ctx)
	return nil
}
