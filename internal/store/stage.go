package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadmgr/internal/lead"
)

var (
	ErrLeadNotFound    = errors.New("lead not found")
	ErrStageNotAllowed = errors.New("stage change not allowed")
)

// ChangeStage moves the lead with the partner's externalID to stage `to` and queues it for sync.
func ChangeStage(ctx context.Context, pool *pgxpool.Pool, externalID string, to lead.Stage) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := New(tx)
		row, err := queries.GetLeadStageForUpdate(ctx, externalID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeadNotFound
		}
		if err != nil {
			return err
		}
		if !lead.Stage(row.Stage).CanMoveTo(to) {
			return ErrStageNotAllowed
		}
		if err := queries.SetLeadStage(ctx, SetLeadStageParams{ID: row.ID, Stage: LeadStage(to)}); err != nil {
			return err
		}
		return queries.MarkLeadForSync(ctx, row.ID)
	})
}
