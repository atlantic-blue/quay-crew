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
func (p *Postgres) Set(ctx context.Context, workspace string, secret Secret) error {
	if workspace == "" {
		return fmt.Errorf("secrets: a secret needs a workspace")
	}
	if err := secret.Validate(); err != nil {
		return err
	}
	sealed, err := Seal(p.key, secret.Value)
	if err != nil {
		return err
	}
	// The projection is updated on conflict too. Setting a secret again is how an operator changes
	// which way it reaches a sandbox, and a stored projection that outlived the value it described
	// would send the new value somewhere the operator did not ask for.
	_, err = p.pool.Exec(ctx, `
		insert into secrets (workspace, name, sealed, projection) values ($1, $2, $3, $4)
		on conflict (workspace, name) do update
			set sealed = excluded.sealed, projection = excluded.projection, updated_at = now()`,
		workspace, secret.Name, sealed, string(secret.Projection.Or()))
	if err != nil {
		// Never the value, and never the sealed bytes: an error is a thing people paste.
		return fmt.Errorf("secrets: storing %s for %s: %w", secret.Name, workspace, err)
	}
	return nil
}

// List says what a workspace has set and how each one reaches a sandbox, sorted, and never what any
// of it says. The sealed bytes are not selected at all, so this call cannot leak one by mistake.
func (p *Postgres) List(ctx context.Context, workspace string) ([]Ref, error) {
	rows, err := p.pool.Query(ctx,
		`select name, projection from secrets where workspace = $1 order by name`, workspace)
	if err != nil {
		return nil, fmt.Errorf("secrets: listing what %s has set: %w", workspace, err)
	}
	defer rows.Close()

	refs := make([]Ref, 0)
	for rows.Next() {
		var ref Ref
		var projection string
		if err := rows.Scan(&ref.Name, &projection); err != nil {
			return nil, fmt.Errorf("secrets: listing what %s has set: %w", workspace, err)
		}
		ref.Projection = Projection(projection).Or()
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secrets: listing what %s has set: %w", workspace, err)
	}
	return refs, nil
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
