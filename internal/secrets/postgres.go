package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps a workspace's credentials where the rest of the crew's durable state is, so the
// subscription token stops being lost on every restart. Values are sealed with a key that lives on
// the host, so this table on its own is worth nothing.
type Postgres struct {
	pool *pgxpool.Pool
	key  []byte
}

var _ Store = (*Postgres)(nil)

// NewPostgres builds a durable secrets store over an existing pool.
func NewPostgres(pool *pgxpool.Pool, key []byte) (*Postgres, error) {
	if pool == nil {
		return nil, fmt.Errorf("secrets: a durable store needs a database")
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("secrets: a key is %d bytes, got %d", KeyLength, len(key))
	}
	return &Postgres{pool: pool, key: key}, nil
}

// Set stores a workspace's secret, sealed.
func (p *Postgres) Set(ctx context.Context, workspace, name, value string) error {
	if workspace == "" || name == "" {
		return fmt.Errorf("secrets: a secret needs a workspace and a name")
	}
	sealed, err := Seal(p.key, value)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		insert into secrets (workspace, name, sealed) values ($1, $2, $3)
		on conflict (workspace, name) do update set sealed = excluded.sealed, updated_at = now()`,
		workspace, name, sealed)
	if err != nil {
		// Never the value, and never the sealed bytes: an error is a thing people paste.
		return fmt.Errorf("secrets: storing %s for %s: %w", name, workspace, err)
	}
	return nil
}

// Get returns a workspace's secret.
func (p *Postgres) Get(ctx context.Context, workspace, name string) (string, error) {
	var sealed []byte
	err := p.pool.QueryRow(ctx,
		`select sealed from secrets where workspace = $1 and name = $2`, workspace, name).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secrets: reading %s for %s: %w", name, workspace, err)
	}
	return Open(p.key, sealed)
}
