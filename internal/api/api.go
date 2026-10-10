package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/sheets"
)

type Deps struct {
	Pool          *pgxpool.Pool
	SigningSecret []byte
	AdminAPIKey   []byte
	Google        *sheets.Google
	Now           func() time.Time
}

func New(d Deps) *fiber.App {
	if d.Now == nil {
		d.Now = time.Now
	}
	app := fiber.New()
	app.Post("/v1/leads", d.postLead)
	app.Patch("/v1/leads/:id/stage", d.requireAdmin, d.patchStage)
	app.Get("/auth/google", d.startGoogleSignIn)
	app.Get("/auth/google/callback", d.finishGoogleSignIn)
	app.Get("/healthz", func(c fiber.Ctx) error {
		if err := d.Pool.Ping(c.Context()); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"database": "down"})
		}
		return c.JSON(fiber.Map{"database": "ok", "google": d.Google.Status(c.Context())})
	})
	return app
}
