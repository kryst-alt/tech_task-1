package sheets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	gsheets "google.golang.org/api/sheets/v4"

	"leadmgr/internal/store"
)

func OAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     google.Endpoint,
		// drive.file only reaches files this app created, which covers the sheet it makes.
		Scopes: []string{"openid", "email", "https://www.googleapis.com/auth/drive.file"},
	}
}

// Google holds the linked account and hands out a Sheet that refreshes its own access token.
type Google struct {
	Pool  *pgxpool.Pool
	OAuth *oauth2.Config
	Key   []byte

	mu       sync.Mutex
	cached   *googleSheet
	cachedAt time.Time
}

func (g *Google) AuthURL(state string) string {
	return g.OAuth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

// Link exchanges the sign-in code, stores the encrypted refresh token and makes sure the sheet exists.
func (g *Google) Link(ctx context.Context, code string) (email, sheetURL string, err error) {
	tok, err := g.OAuth.Exchange(ctx, code)
	if err != nil {
		return "", "", fmt.Errorf("exchange code: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", "", errors.New("google returned no refresh token")
	}
	email, err = idTokenEmail(tok)
	if err != nil {
		return "", "", err
	}
	sealed, err := seal(g.Key, []byte(tok.RefreshToken))
	if err != nil {
		return "", "", err
	}
	if err := store.New(g.Pool).SaveGoogleCredentials(ctx, store.SaveGoogleCredentialsParams{
		AccountEmail: email, RefreshTokenEnc: sealed,
	}); err != nil {
		return "", "", err
	}
	s, err := g.sheet(ctx)
	if err != nil {
		return "", "", err
	}
	return email, "https://docs.google.com/spreadsheets/d/" + s.id, nil
}

// Status is "not_linked", "reauth_required" or "ok".
func (g *Google) Status(ctx context.Context) string {
	creds, err := store.New(g.Pool).GetGoogleCredentials(ctx)
	switch {
	case err != nil:
		return "not_linked"
	case creds.NeedsReauth:
		return "reauth_required"
	}
	return "ok"
}

func (g *Google) Sheet(ctx context.Context) (Sheet, error) {
	return g.sheet(ctx)
}

func (g *Google) sheet(ctx context.Context) (*googleSheet, error) {
	q := store.New(g.Pool)
	creds, err := q.GetGoogleCredentials(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	if creds.NeedsReauth {
		return nil, ErrReauthRequired
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cached != nil && g.cachedAt.Equal(creds.UpdatedAt) {
		return g.cached, nil
	}

	refresh, err := open(g.Key, creds.RefreshTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}
	// The token source renews the hourly access token from the refresh token on its own.
	ts := g.OAuth.TokenSource(context.Background(), &oauth2.Token{RefreshToken: string(refresh)})
	svc, err := gsheets.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, err
	}

	s := &googleSheet{svc: svc, id: creds.SpreadsheetID.String}
	if !creds.SpreadsheetID.Valid {
		if s.id, err = createSpreadsheet(ctx, svc); err != nil {
			return nil, classify(err)
		}
		if err := q.SetSpreadsheetID(ctx, s.id); err != nil {
			return nil, err
		}
	}
	g.cached, g.cachedAt = s, creds.UpdatedAt
	return s, nil
}

func createSpreadsheet(ctx context.Context, svc *gsheets.Service) (string, error) {
	ss, err := svc.Spreadsheets.Create(&gsheets.Spreadsheet{
		Properties: &gsheets.SpreadsheetProperties{Title: "Leads"},
		Sheets:     []*gsheets.Sheet{{Properties: &gsheets.SheetProperties{Title: "Leads"}}},
	}).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	_, err = svc.Spreadsheets.Values.Update(ss.SpreadsheetId, "Leads!A1:H1",
		&gsheets.ValueRange{Values: [][]any{header}}).ValueInputOption("RAW").Context(ctx).Do()
	return ss.SpreadsheetId, err
}

type googleSheet struct {
	svc *gsheets.Service
	id  string
}

func (s *googleSheet) UpsertRow(ctx context.Context, r []any) error {
	col, err := s.svc.Spreadsheets.Values.Get(s.id, "Leads!A:A").Context(ctx).Do()
	if err != nil {
		return classify(err)
	}
	vr := &gsheets.ValueRange{Values: [][]any{r}}
	if n := rowNumber(col.Values, r[0].(string)); n > 0 {
		_, err = s.svc.Spreadsheets.Values.Update(s.id, fmt.Sprintf("Leads!A%d:H%d", n, n), vr).
			ValueInputOption("RAW").Context(ctx).Do()
	} else {
		_, err = s.svc.Spreadsheets.Values.Append(s.id, "Leads!A:H", vr).
			ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	}
	return classify(err)
}

// rowNumber returns the 1-based sheet row holding id in column A, or 0. Row 1 is the header.
func rowNumber(colA [][]any, id string) int {
	for i, r := range colA {
		if i > 0 && len(r) > 0 && fmt.Sprint(r[0]) == id {
			return i + 1
		}
	}
	return 0
}

func classify(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		return fmt.Errorf("%w: %v", ErrReauthRequired, err)
	}
	return err
}

// idTokenEmail reads the email claim. The token came straight from Google's
// token endpoint over TLS, so its signature does not need checking here.
func idTokenEmail(tok *oauth2.Token) (string, error) {
	raw, _ := tok.Extra("id_token").(string)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("google returned no id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct{ Email string }
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Email == "" {
		return "", errors.New("id_token has no email")
	}
	return claims.Email, nil
}

func seal(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func open(key, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("sealed value too short")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
