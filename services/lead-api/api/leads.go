package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"leadmgr/internal/lead"
	"leadmgr/internal/store"
)

var errBadSignature = errors.New("invalid signature")

// verifySignature checks X-Signature = "sha256=" + hex(HMAC-SHA256(secret, timestamp + "." + body)).
func verifySignature(secret []byte, timestamp, signature string, body []byte, now time.Time, tolerance time.Duration) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return errBadSignature
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || !strings.HasPrefix(signature, "sha256=") {
		return errBadSignature
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	if !hmac.Equal(got, m.Sum(nil)) {
		return errBadSignature
	}
	return nil
}

func parseLead(body []byte) (lead.Incoming, error) {
	var raw struct {
		ID, Name, Email, Company, Source string
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return lead.Incoming{}, errors.New("body must be a JSON object")
	}
	in := lead.Incoming{
		ExternalID: strings.TrimSpace(raw.ID),
		Fields: lead.Fields{
			Name:    strings.TrimSpace(raw.Name),
			Email:   strings.TrimSpace(raw.Email),
			Company: strings.TrimSpace(raw.Company),
			Source:  strings.TrimSpace(raw.Source),
		},
	}
	var missing []string
	for _, f := range []struct{ name, value string }{
		{"id", in.ExternalID}, {"name", in.Name}, {"email", in.Email}, {"company", in.Company}, {"source", in.Source},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return lead.Incoming{}, errors.New("required: " + strings.Join(missing, ", "))
	}
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		return lead.Incoming{}, errors.New("email is not a valid address")
	}
	return in, nil
}

var actionStatus = map[lead.Action]int{
	lead.Create:    fiber.StatusCreated,
	lead.Update:    fiber.StatusOK,
	lead.Unchanged: fiber.StatusOK,
	lead.Conflict:  fiber.StatusConflict,
}

func (d Deps) postLead(c fiber.Ctx) error {
	if err := verifySignature(d.SigningSecret, c.Get("X-Timestamp"), c.Get("X-Signature"), c.BodyRaw(), d.Now(), d.SignatureTolerance); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
	}
	in, err := parseLead(c.Body())
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}
	action, err := store.UpsertLead(c.Context(), d.Pool, in)
	if err != nil {
		return err
	}
	return c.Status(actionStatus[action]).JSON(fiber.Map{"result": action.String()})
}
