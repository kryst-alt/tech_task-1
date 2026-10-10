package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"leadmgr/internal/lead"
	"leadmgr/internal/store"
)

func (d Deps) requireAdmin(c fiber.Ctx) error {
	token, ok := strings.CutPrefix(c.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), d.AdminAPIKey) != 1 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid admin token"})
	}
	return c.Next()
}

func (d Deps) patchStage(c fiber.Ctx) error {
	var body struct{ Stage string }
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "body must be a JSON object"})
	}
	to, err := lead.ParseStage(body.Stage)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}
	switch err := store.ChangeStage(c.Context(), d.Pool, c.Params("id"), to); {
	case errors.Is(err, store.ErrLeadNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, store.ErrStageNotAllowed):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case err != nil:
		return err
	}
	return c.JSON(fiber.Map{"stage": to})
}
