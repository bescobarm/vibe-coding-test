package simulator

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	insertCardSQL    = `INSERT INTO cards (card_id, company_id, cardholder_id, status) VALUES ($1, $2, $3, 'active')`
	cardholderPrefix = "holder-"
)

// PostgresCards creates the run's cards directly in the engine's database
// (D-066). It is a development tool; the engine has no card-creation API.
type PostgresCards struct {
	pool *pgxpool.Pool
}

// NewPostgresCards returns a CardCreator on pool.
func NewPostgresCards(pool *pgxpool.Pool) *PostgresCards {
	return &PostgresCards{pool: pool}
}

// CreateCards implements CardCreator: one active card, and one cardholder, per ID.
func (p *PostgresCards) CreateCards(ctx context.Context, companyID string, cardIDs []string) error {
	batch := &pgx.Batch{}

	for _, cardID := range cardIDs {
		batch.Queue(insertCardSQL, cardID, companyID, cardholderPrefix+cardID)
	}

	err := p.pool.SendBatch(ctx, batch).Close()
	if err != nil {
		return fmt.Errorf("insert cards: %w", err)
	}

	return nil
}
