package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/lead"
)

// UpsertLead saves an incoming lead and queues it for Sheets sync in one transaction.
func UpsertLead(ctx context.Context, pool *pgxpool.Pool, in lead.Incoming) (lead.Action, error) {
	action, err := upsertOnce(ctx, pool, in)
	// Two first-time submissions racing: the loser hits a unique index and
	// retries, by which time the winner's row is visible.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		action, err = upsertOnce(ctx, pool, in)
	}
	return action, err
}

func upsertOnce(ctx context.Context, pool *pgxpool.Pool, in lead.Incoming) (action lead.Action, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := New(tx)
		byID, err := lookup(q.GetLeadByExternalIDForUpdate(ctx, in.ExternalID))
		if err != nil {
			return err
		}
		byEmail, err := lookup(q.GetLeadByEmailForUpdate(ctx, in.Email))
		if err != nil {
			return err
		}

		var target *lead.Record
		action, target = lead.Decide(byID, byEmail, in)
		switch action {
		case lead.Create:
			row, err := q.InsertLead(ctx, InsertLeadParams{
				ExternalID: in.ExternalID, Email: in.Email, Name: in.Name, Company: in.Company, Source: in.Source,
			})
			if err != nil {
				return err
			}
			return q.MarkLeadForSync(ctx, row.ID)
		case lead.Update:
			if err := q.UpdateLeadFields(ctx, UpdateLeadFieldsParams{
				ID: target.ID, Email: in.Email, Name: in.Name, Company: in.Company, Source: in.Source,
			}); err != nil {
				return err
			}
			return q.MarkLeadForSync(ctx, target.ID)
		}
		return nil
	})
	return action, err
}

func lookup(row Lead, err error) (*lead.Record, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lead.Record{ID: row.ID, Fields: lead.Fields{
		Name: row.Name, Email: row.Email, Company: row.Company, Source: row.Source,
	}}, nil
}
