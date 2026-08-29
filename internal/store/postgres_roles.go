package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ImportRole takes a role into the crew, refusing a version that already exists carrying something
// different.
func (p *Postgres) ImportRole(ctx context.Context, imported ImportedRole) error {
	var held string
	err := p.pool.QueryRow(ctx,
		`select fingerprint from roles where name = $1 and version = $2`,
		imported.Name, imported.Version).Scan(&held)
	switch {
	case err == nil && held == imported.Fingerprint():
		// The same role, imported twice. Importing is how a role arrives from a repository, and
		// doing it again after a pull that changed nothing should not be an error.
		return nil
	case err == nil:
		return fmt.Errorf("%w: %s version %d", ErrRoleChanged, imported.Name, imported.Version)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read role %s: %w", imported.Name, err)
	}

	if _, err := p.pool.Exec(ctx, `
		insert into roles (name, version, summary, model, receives, "may", brief, fingerprint)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		imported.Name, imported.Version, imported.Summary, imported.Model,
		textArray(imported.Receives), textArray(imported.May_), imported.Brief,
		imported.Fingerprint()); err != nil {
		return fmt.Errorf("import role %s: %w", imported.Name, err)
	}
	return nil
}

// GetRole returns one revision of a role.
func (p *Postgres) GetRole(ctx context.Context, name string, version int) (ImportedRole, error) {
	return p.roleRow(ctx, `where name = $1 and version = $2`, name, version)
}

// ListRoles returns the newest revision of every role.
func (p *Postgres) ListRoles(ctx context.Context) ([]ImportedRole, error) {
	return p.roleRows(ctx, `
		select r.name, r.version, r.summary, r.model, r.receives, r."may", r.brief, r.imported_at
		from roles r
		join (select name, max(version) as version from roles group by name) newest
		  on newest.name = r.name and newest.version = r.version
		order by r.name`)
}

// AttachRole gives a workspace a role at the newest revision the crew holds.
func (p *Postgres) AttachRole(ctx context.Context, workspace, name string) (ImportedRole, error) {
	if _, err := p.GetWorkspace(ctx, workspace); err != nil {
		return ImportedRole{}, err
	}
	newest, err := p.roleRow(ctx, `where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return ImportedRole{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_roles (workspace, name, version) values ($1, $2, $3)
		on conflict (workspace, name) do update
		  set version = excluded.version, attached_at = now()`,
		workspace, newest.Name, newest.Version); err != nil {
		return ImportedRole{}, fmt.Errorf("attach role %s: %w", name, err)
	}
	return newest, nil
}

// DetachRole takes a role away from a workspace.
func (p *Postgres) DetachRole(ctx context.Context, workspace, name string) error {
	tag, err := p.pool.Exec(ctx,
		`delete from workspace_roles where workspace = $1 and name = $2`, workspace, name)
	if err != nil {
		return fmt.Errorf("detach role %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WorkspaceRoles returns what a workspace holds, at the versions it pinned.
func (p *Postgres) WorkspaceRoles(ctx context.Context, workspace string) ([]ImportedRole, error) {
	return p.roleRows(ctx, `
		select r.name, r.version, r.summary, r.model, r.receives, r."may", r.brief, r.imported_at
		from workspace_roles w
		join roles r on r.name = w.name and r.version = w.version
		where w.workspace = $1
		order by r.name`, workspace)
}

// AttachCrewRole gives the whole crew a role at the newest revision it holds.
func (p *Postgres) AttachCrewRole(ctx context.Context, name string) (ImportedRole, error) {
	newest, err := p.roleRow(ctx, `where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return ImportedRole{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into crew_roles (name, version) values ($1, $2)
		on conflict (name) do update
		  set version = excluded.version, attached_at = now()`,
		newest.Name, newest.Version); err != nil {
		return ImportedRole{}, fmt.Errorf("attach crew role %s: %w", name, err)
	}
	return newest, nil
}

// DetachCrewRole takes a role away from the crew.
func (p *Postgres) DetachCrewRole(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `delete from crew_roles where name = $1`, name)
	if err != nil {
		return fmt.Errorf("detach crew role %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CrewRoles returns what the crew holds, at the versions it pinned.
func (p *Postgres) CrewRoles(ctx context.Context) ([]ImportedRole, error) {
	return p.roleRows(ctx, `
		select r.name, r.version, r.summary, r.model, r.receives, r."may", r.brief, r.imported_at
		from crew_roles c
		join roles r on r.name = c.name and r.version = c.version
		order by r.name`)
}

// roleRow reads one role.
func (p *Postgres) roleRow(ctx context.Context, where string, args ...any) (ImportedRole, error) {
	var held ImportedRole
	err := p.pool.QueryRow(ctx, `
		select name, version, summary, model, receives, "may", brief, imported_at from roles `+where, args...).
		Scan(&held.Name, &held.Version, &held.Summary, &held.Model, &held.Receives, &held.May_,
			&held.Brief, &held.ImportedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportedRole{}, ErrNotFound
	}
	if err != nil {
		return ImportedRole{}, fmt.Errorf("read role: %w", err)
	}
	return held, nil
}

// roleRows reads a listing of roles, whichever level asked for it.
func (p *Postgres) roleRows(ctx context.Context, query string, args ...any) ([]ImportedRole, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var out []ImportedRole
	for rows.Next() {
		var held ImportedRole
		if err := rows.Scan(&held.Name, &held.Version, &held.Summary, &held.Model, &held.Receives,
			&held.May_, &held.Brief, &held.ImportedAt); err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return out, nil
}
