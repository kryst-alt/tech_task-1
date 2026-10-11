package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v3"
)

const stateCookie = "oauth_state"

// startGoogleSignIn takes the admin key as a query parameter because a browser
// link cannot send an Authorization header.
func (d Deps) startGoogleSignIn(c fiber.Ctx) error {
	if !d.adminKeyOK(c.Query("key")) {
		return c.Status(fiber.StatusUnauthorized).SendString("missing or wrong ?key= (use ADMIN_API_KEY)")
	}
	slog.Info("google sign-in started")
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)
	c.Cookie(&fiber.Cookie{Name: stateCookie, Value: state, Path: "/auth/google", MaxAge: 600, HTTPOnly: true, SameSite: "Lax"})
	return c.Redirect().Status(fiber.StatusFound).To(d.Google.AuthURL(state))
}

func (d Deps) finishGoogleSignIn(c fiber.Ctx) error {
	want := c.Cookies(stateCookie)
	if want == "" || subtle.ConstantTimeCompare([]byte(c.Query("state")), []byte(want)) != 1 {
		slog.Warn("google callback rejected: state mismatch", "has_cookie", want != "", "google_error", c.Query("error"))
		return c.Status(fiber.StatusBadRequest).SendString("sign-in expired or was not started here, open /auth/google again")
	}
	c.ClearCookie(stateCookie)
	if googleError := c.Query("error"); googleError != "" {
		slog.Warn("google sign-in refused", "error", googleError)
		return c.Status(fiber.StatusBadRequest).SendString("google sign-in failed: " + googleError)
	}
	email, sheetURL, err := d.Google.Link(c.Context(), c.Query("code"))
	if err != nil {
		slog.Error("google link", "err", err)
		return c.Status(fiber.StatusBadGateway).SendString("could not link google account: " + err.Error())
	}
	slog.Info("google account linked", "email", email, "sheet", sheetURL)
	return c.SendString(fmt.Sprintf("Connected %s. Leads sync to %s\n", email, sheetURL))
}
