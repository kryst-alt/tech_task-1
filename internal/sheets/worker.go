package sheets

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/store"
)

const batchSize = 50

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
	Pool     *pgxpool.Pool
	SheetFor func(context.Context) (Sheet, error)
	Interval time.Duration
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
	q := store.New(w.Pool)
	sheet, err := w.SheetFor(ctx)
	switch {
	case errors.Is(err, ErrNotLinked):
		return 0, nil
	case errors.Is(err, ErrReauthRequired):
		return 0, w.flagReauth(ctx, q, err)
	case err != nil:
		return 0, err
	}

	due, err := q.DueSyncs(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	synced := 0
	for _, l := range due {
		err := sheet.UpsertRow(ctx, row(l))
		switch {
		case errors.Is(err, ErrReauthRequired):
			return synced, w.flagReauth(ctx, q, err)
		case err != nil:
			slog.Warn("sheets sync failed, will retry", "lead", l.ExternalID, "err", err)
			if err := q.MarkSyncFailed(ctx, store.MarkSyncFailedParams{LeadID: l.ID, LastError: err.Error()}); err != nil {
				return synced, err
			}
		default:
			if err := q.MarkSynced(ctx, store.MarkSyncedParams{LeadID: l.ID, SyncedVersion: l.Version}); err != nil {
				return synced, err
			}
			synced++
		}
	}
	return synced, nil
}

func (w *Worker) flagReauth(ctx context.Context, q *store.Queries, cause error) error {
	n, err := q.MarkGoogleNeedsReauth(ctx)
	if n == 1 {
		slog.Error("google sign-in needed again, sync paused", "err", cause)
	}
	return err
}

var header = []any{"Lead ID", "Name", "Email", "Company", "Source", "Stage", "Created", "Updated"}

func row(l store.Lead) []any {
	return []any{
		l.ExternalID, l.Name, l.Email, l.Company, l.Source, string(l.Stage),
		l.CreatedAt.UTC().Format(time.RFC3339), l.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
