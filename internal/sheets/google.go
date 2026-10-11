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
	spreadsheet, err := g.sheet(ctx)
	if err != nil {
		return "", "", err
	}
	return email, "https://docs.google.com/spreadsheets/d/" + spreadsheet.id, nil
}

// Status is "not_linked", "reauth_required", "ok", or "unknown" when the database can't say.
func (g *Google) Status(ctx context.Context) string {
	creds, err := store.New(g.Pool).GetGoogleCredentials(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "not_linked"
	case err != nil:
		return "unknown"
	case creds.NeedsReauth:
		return "reauth_required"
	}
	return "ok"
}

func (g *Google) Sheet(ctx context.Context) (Sheet, error) {
	return g.sheet(ctx)
}

func (g *Google) sheet(ctx context.Context) (*googleSheet, error) {
	queries := store.New(g.Pool)
	creds, err := queries.GetGoogleCredentials(ctx)
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
	tokenSource := g.OAuth.TokenSource(context.Background(), &oauth2.Token{RefreshToken: string(refresh)})
	svc, err := gsheets.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, err
	}

	spreadsheet := &googleSheet{svc: svc, id: creds.SpreadsheetID.String}
	if !creds.SpreadsheetID.Valid {
		if spreadsheet.id, err = createSpreadsheet(ctx, svc); err != nil {
			return nil, classify(err)
		}
		if err := queries.SetSpreadsheetID(ctx, spreadsheet.id); err != nil {
			return nil, err
		}
	}
	g.cached, g.cachedAt = spreadsheet, creds.UpdatedAt
	return spreadsheet, nil
}

func createSpreadsheet(ctx context.Context, svc *gsheets.Service) (string, error) {
	cells := make([]*gsheets.CellData, len(header))
	for i, h := range header {
		title := h.(string)
		cells[i] = &gsheets.CellData{UserEnteredValue: &gsheets.ExtendedValue{StringValue: &title}}
	}
	ss, err := svc.Spreadsheets.Create(&gsheets.Spreadsheet{
		Properties: &gsheets.SpreadsheetProperties{Title: "Leads"},
		Sheets: []*gsheets.Sheet{{
			Properties: &gsheets.SheetProperties{Title: "Leads"},
			Data:       []*gsheets.GridData{{RowData: []*gsheets.RowData{{Values: cells}}}},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return ss.SpreadsheetId, nil
}

type googleSheet struct {
	svc *gsheets.Service
	id  string
}

func (s *googleSheet) UpsertRow(ctx context.Context, values []any) error {
	col, err := s.svc.Spreadsheets.Values.Get(s.id, "Leads!A:A").Context(ctx).Do()
	if err != nil {
		return classify(err)
	}
	valueRange := &gsheets.ValueRange{Values: [][]any{values}}
	if rowNum := rowNumber(col.Values, values[0].(string)); rowNum > 0 {
		_, err = s.svc.Spreadsheets.Values.Update(s.id, fmt.Sprintf("Leads!A%d:H%d", rowNum, rowNum), valueRange).
			ValueInputOption("RAW").Context(ctx).Do()
	} else {
		_, err = s.svc.Spreadsheets.Values.Append(s.id, "Leads!A:H", valueRange).
			ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	}
	return classify(err)
}

// rowNumber returns the 1-based sheet row holding id in column A, or 0. Row 1 is the header.
func rowNumber(colA [][]any, id string) int {
	for i, cells := range colA {
		if i > 0 && len(cells) > 0 && fmt.Sprint(cells[0]) == id {
			return i + 1
		}
	}
	return 0
}

func classify(err error) error {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
		return fmt.Errorf("%w: %v", ErrReauthRequired, err)
	}
	return err
}

// idTokenEmail reads the email claim. The token came straight from Google's
// token endpoint over TLS, so its signature does not need checking here.
func idTokenEmail(tok *oauth2.Token) (string, error) {
	idToken, _ := tok.Extra("id_token").(string)
	parts := strings.Split(idToken, ".")
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
