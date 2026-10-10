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

	"leadmgr/internal/api"
	"leadmgr/internal/config"
	"leadmgr/internal/sheets"
	"leadmgr/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	google := &sheets.Google{
		Pool:  pool,
		OAuth: sheets.OAuthConfig(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL),
		Key:   cfg.TokenKey,
	}
	worker := &sheets.Worker{Pool: pool, SheetFor: google.Sheet, Interval: 2 * time.Second}
	go worker.Run(ctx)

	app := api.New(api.Deps{Pool: pool, SigningSecret: cfg.SigningSecret, AdminAPIKey: cfg.AdminAPIKey, Google: google})

	errc := make(chan error, 1)
	go func() {
		errc <- app.Listen(fmt.Sprintf(":%d", cfg.Port), fiber.ListenConfig{DisableStartupMessage: true})
	}()
	slog.Info("listening", "port", cfg.Port)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.ShutdownWithContext(shutdownCtx)
}
