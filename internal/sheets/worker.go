package sheets

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/store"
)

var (
	ErrNotLinked      = errors.New("google account not linked")
	ErrReauthRequired = errors.New("google sign-in expired or revoked")
)

// Sheet writes one lead row, replacing the row with the same lead ID if there is one.
type Sheet interface {
	UpsertRow(ctx context.Context, row []any) error
}

// Worker copies leads whose version is ahead of what the sheet has.
type Worker struct {
	Pool      *pgxpool.Pool
	SheetFor  func(context.Context) (Sheet, error)
	Interval  time.Duration
	BatchSize int32 // defaults to 50
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		if _, err := w.SyncDue(ctx); err != nil && ctx.Err() == nil {
			slog.Error("sheets sync", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SyncDue writes every lead that is due and returns how many were written.
// No transaction is held across Google calls, so a slow API never blocks lead
// intake. Marking a sync keeps the highest version, so a lead changed mid-call
// stays due.
func (w *Worker) SyncDue(ctx context.Context) (int, error) {
	queries := store.New(w.Pool)
	sheet, err := w.SheetFor(ctx)
	switch {
	case errors.Is(err, ErrNotLinked):
		return 0, nil
	case errors.Is(err, ErrReauthRequired):
		return 0, w.flagReauth(ctx, queries, err)
	case err != nil:
		return 0, err
	}

	batch := w.BatchSize
	if batch == 0 {
		batch = 50
	}
	due, err := queries.DueSyncs(ctx, batch)
	if err != nil {
		return 0, err
	}
	synced := 0
	for _, dueLead := range due {
		err := sheet.UpsertRow(ctx, row(dueLead))
		switch {
		case errors.Is(err, ErrReauthRequired):
			return synced, w.flagReauth(ctx, queries, err)
		case err != nil:
			slog.Warn("sheets sync failed, will retry", "lead", dueLead.ExternalID, "err", err)
			if err := queries.MarkSyncFailed(ctx, store.MarkSyncFailedParams{LeadID: dueLead.ID, LastError: err.Error()}); err != nil {
				return synced, err
			}
		default:
			if err := queries.MarkSynced(ctx, store.MarkSyncedParams{LeadID: dueLead.ID, SyncedVersion: dueLead.Version}); err != nil {
				return synced, err
			}
			synced++
		}
	}
	return synced, nil
}

func (w *Worker) flagReauth(ctx context.Context, queries *store.Queries, cause error) error {
	n, err := queries.MarkGoogleNeedsReauth(ctx)
	if n == 1 {
		slog.Error("google sign-in needed again, sync paused", "err", cause)
	}
	return err
}

var header = []any{"Lead ID", "Name", "Email", "Company", "Source", "Stage", "Created", "Updated"}

func row(record store.Lead) []any {
	return []any{
		record.ExternalID, record.Name, record.Email, record.Company, record.Source, string(record.Stage),
		record.CreatedAt.UTC().Format(time.RFC3339), record.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
