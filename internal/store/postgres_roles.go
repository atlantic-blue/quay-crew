package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		//
		// Where it was read stays current, because that is the answer that moves: committing a role
		// somebody kept in a folder and importing it again is how the warning is cleared, and a row
		// that kept the first answer would leave the operator fixing it and watching nothing change.
		return p.roleReadAt(ctx, imported)
	case err == nil:
		return fmt.Errorf("%w: %s version %d", ErrRoleChanged, imported.Name, imported.Version)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read role %s: %w", imported.Name, err)
	}

	if _, err := p.pool.Exec(ctx, `
		insert into roles (name, version, summary, model, receives, "may", brief, fingerprint,
		                   origin_repository, origin_commit, origin_path, origin_dirty, origin_unpushed)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		imported.Name, imported.Version, imported.Summary, imported.Model,
		textArray(imported.Receives), textArray(imported.May_), imported.Brief,
		imported.Fingerprint(), imported.Origin.Repository, imported.Origin.Commit,
		imported.Origin.Path, imported.Origin.Dirty, imported.Origin.Unpushed); err != nil {
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
		select `+roleColumns("r")+`
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
		select `+roleColumns("r")+`
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
		select `+roleColumns("r")+`
		from crew_roles c
		join roles r on r.name = c.name and r.version = c.version
		order by r.name`)
}

// roleRow reads one role.
func (p *Postgres) roleRow(ctx context.Context, where string, args ...any) (ImportedRole, error) {
	var held ImportedRole
	err := p.pool.QueryRow(ctx,
		`select `+roleColumns("")+` from roles `+where, args...).Scan(intoRole(&held)...)
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
		if err := rows.Scan(intoRole(&held)...); err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return out, nil
}

// roleColumns is every column a role is read out of, written once.
//
// Four queries read a role, and a column added to three of them is a field that survives one listing
// and not the next: what a role may call went missing in exactly that shape and only Postgres knew.
// alias is what the roles table is called in the query, empty where it is not aliased.
func roleColumns(alias string) string {
	if alias != "" {
		alias += "."
	}
	columns := []string{"name", "version", "summary", "model", "receives", `"may"`, "brief",
		"imported_at", "origin_repository", "origin_commit", "origin_path", "origin_dirty",
		"origin_unpushed"}
	for at, column := range columns {
		columns[at] = alias + column
	}
	return strings.Join(columns, ", ")
}

// intoRole is where each of those columns lands, in the same order. The two belong together, so
// they are next to each other rather than in the four places a role is read.
func intoRole(held *ImportedRole) []any {
	return []any{&held.Name, &held.Version, &held.Summary, &held.Model, &held.Receives, &held.May_,
		&held.Brief, &held.ImportedAt, &held.Origin.Repository, &held.Origin.Commit,
		&held.Origin.Path, &held.Origin.Dirty, &held.Origin.Unpushed}
}

// roleReadAt records where a role the crew already holds was read this time.
//
// The role itself is untouched: the fingerprint matched, so these are the same bytes, and only the
// question of who else can read them has an answer that moves.
func (p *Postgres) roleReadAt(ctx context.Context, imported ImportedRole) error {
	if _, err := p.pool.Exec(ctx, `
		update roles
		   set origin_repository = $3, origin_commit = $4, origin_path = $5,
		       origin_dirty = $6, origin_unpushed = $7
		 where name = $1 and version = $2`,
		imported.Name, imported.Version, imported.Origin.Repository, imported.Origin.Commit,
		imported.Origin.Path, imported.Origin.Dirty, imported.Origin.Unpushed); err != nil {
		return fmt.Errorf("record where role %s was read: %w", imported.Name, err)
	}
	return nil
}
