package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/jackc/pgx/v5"
)

// ImportSkill takes a skill into the crew, in one transaction, refusing a version that already exists
// carrying something different.
//
// The whole skill goes in together: a skill whose manifest landed and whose files did not is a
// capability that says it can do something and has no script to do it with.
func (p *Postgres) ImportSkill(ctx context.Context, imported Imported) error {
	var held string
	err := p.pool.QueryRow(ctx,
		`select fingerprint from skills where name = $1 and version = $2`,
		imported.Name, imported.Version).Scan(&held)
	switch {
	case err == nil && held == imported.Fingerprint():
		// The same skill, imported twice. Importing is how a skill arrives from a repository, and doing
		// it again after a pull that changed nothing should not be an error.
		return nil
	case err == nil:
		return fmt.Errorf("%w: %s version %d", ErrSkillChanged, imported.Name, imported.Version)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read skill %s: %w", imported.Name, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("import skill %s: %w", imported.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into skills (name, version, summary, binaries, brief, fingerprint)
		values ($1, $2, $3, $4, $5, $6)`,
		imported.Name, imported.Version, imported.Summary, textArray(imported.Binaries), imported.Brief,
		imported.Fingerprint()); err != nil {
		return fmt.Errorf("import skill %s: %w", imported.Name, err)
	}
	for _, name := range imported.SecretNames() {
		if _, err := tx.Exec(ctx, `
			insert into skill_secrets (name, version, secret, purpose) values ($1, $2, $3, $4)`,
			imported.Name, imported.Version, name, imported.Secrets[name]); err != nil {
			return fmt.Errorf("import skill %s secret %s: %w", imported.Name, name, err)
		}
	}
	for _, file := range imported.Files {
		if _, err := tx.Exec(ctx, `
			insert into skill_files (name, version, path, body, executable) values ($1, $2, $3, $4, $5)`,
			imported.Name, imported.Version, file.Path, file.Body, file.Executable); err != nil {
			return fmt.Errorf("import skill %s file %s: %w", imported.Name, file.Path, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("import skill %s: %w", imported.Name, err)
	}
	return nil
}

// GetSkill returns one revision of a skill, files included.
func (p *Postgres) GetSkill(ctx context.Context, name string, version int) (Imported, error) {
	held, err := p.skillRow(ctx, `where name = $1 and version = $2`, name, version)
	if err != nil {
		return Imported{}, err
	}
	if err := p.fillSkill(ctx, &held); err != nil {
		return Imported{}, err
	}
	return held, nil
}

// ListSkills returns the newest revision of every skill, without their files.
//
// Without the files deliberately. A listing is read to see what the crew can do, and the files are by
// far the largest part of a skill, so carrying them would make the cheapest call the most expensive.
func (p *Postgres) ListSkills(ctx context.Context) ([]Imported, error) {
	rows, err := p.pool.Query(ctx, `
		select s.name, s.version, s.summary, s.binaries, s.brief, s.imported_at
		from skills s
		join (select name, max(version) as version from skills group by name) newest
		  on newest.name = s.name and newest.version = s.version
		order by s.name`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	var out []Imported
	for rows.Next() {
		var held Imported
		if err := rows.Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries, &held.Brief,
			&held.ImportedAt); err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	for at := range out {
		if err := p.fillSecrets(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AttachSkill gives a workspace a skill at the newest revision the crew holds.
func (p *Postgres) AttachSkill(ctx context.Context, workspace, name string) (Imported, error) {
	if _, err := p.GetWorkspace(ctx, workspace); err != nil {
		return Imported{}, err
	}
	newest, err := p.skillRow(ctx,
		`where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return Imported{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_skills (workspace, name, version) values ($1, $2, $3)
		on conflict (workspace, name) do update
		  set version = excluded.version, attached_at = now()`,
		workspace, newest.Name, newest.Version); err != nil {
		return Imported{}, fmt.Errorf("attach skill %s: %w", name, err)
	}
	if err := p.fillSkill(ctx, &newest); err != nil {
		return Imported{}, err
	}
	return newest, nil
}

// DetachSkill takes a skill away from a workspace.
func (p *Postgres) DetachSkill(ctx context.Context, workspace, name string) error {
	tag, err := p.pool.Exec(ctx,
		`delete from workspace_skills where workspace = $1 and name = $2`, workspace, name)
	if err != nil {
		return fmt.Errorf("detach skill %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WorkspaceSkills returns what a workspace holds, at the versions it pinned, files included.
func (p *Postgres) WorkspaceSkills(ctx context.Context, workspace string) ([]Imported, error) {
	rows, err := p.pool.Query(ctx, `
		select s.name, s.version, s.summary, s.binaries, s.brief, s.imported_at
		from workspace_skills w
		join skills s on s.name = w.name and s.version = w.version
		where w.workspace = $1
		order by s.name`, workspace)
	if err != nil {
		return nil, fmt.Errorf("list workspace skills: %w", err)
	}
	defer rows.Close()

	var out []Imported
	for rows.Next() {
		var held Imported
		if err := rows.Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries, &held.Brief,
			&held.ImportedAt); err != nil {
			return nil, fmt.Errorf("list workspace skills: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workspace skills: %w", err)
	}
	for at := range out {
		if err := p.fillSkill(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AttachCrewSkill gives the whole crew a skill at the newest revision it holds.
func (p *Postgres) AttachCrewSkill(ctx context.Context, name string) (Imported, error) {
	newest, err := p.skillRow(ctx, `where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return Imported{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into crew_skills (name, version) values ($1, $2)
		on conflict (name) do update
		  set version = excluded.version, attached_at = now()`,
		newest.Name, newest.Version); err != nil {
		return Imported{}, fmt.Errorf("attach crew skill %s: %w", name, err)
	}
	if err := p.fillSkill(ctx, &newest); err != nil {
		return Imported{}, err
	}
	return newest, nil
}

// DetachCrewSkill takes a skill away from the crew.
func (p *Postgres) DetachCrewSkill(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `delete from crew_skills where name = $1`, name)
	if err != nil {
		return fmt.Errorf("detach crew skill %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CrewSkills returns what the crew holds, at the versions it pinned, files included.
func (p *Postgres) CrewSkills(ctx context.Context) ([]Imported, error) {
	rows, err := p.pool.Query(ctx, `
		select s.name, s.version, s.summary, s.binaries, s.brief, s.imported_at
		from crew_skills c
		join skills s on s.name = c.name and s.version = c.version
		order by s.name`)
	if err != nil {
		return nil, fmt.Errorf("list crew skills: %w", err)
	}
	defer rows.Close()

	var out []Imported
	for rows.Next() {
		var held Imported
		if err := rows.Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries, &held.Brief,
			&held.ImportedAt); err != nil {
			return nil, fmt.Errorf("list crew skills: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list crew skills: %w", err)
	}
	for at := range out {
		if err := p.fillSkill(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// skillRow reads one skill's own row, without its secrets or its files.
func (p *Postgres) skillRow(ctx context.Context, where string, args ...any) (Imported, error) {
	var held Imported
	err := p.pool.QueryRow(ctx, `
		select name, version, summary, binaries, brief, imported_at from skills `+where, args...).
		Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries, &held.Brief, &held.ImportedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Imported{}, ErrNotFound
	}
	if err != nil {
		return Imported{}, fmt.Errorf("read skill: %w", err)
	}
	return held, nil
}

// fillSkill adds a skill's secrets and its files.
func (p *Postgres) fillSkill(ctx context.Context, held *Imported) error {
	if err := p.fillSecrets(ctx, held); err != nil {
		return err
	}
	return p.fillFiles(ctx, held)
}

func (p *Postgres) fillSecrets(ctx context.Context, held *Imported) error {
	rows, err := p.pool.Query(ctx, `
		select secret, purpose from skill_secrets where name = $1 and version = $2 order by secret`,
		held.Name, held.Version)
	if err != nil {
		return fmt.Errorf("read skill %s secrets: %w", held.Name, err)
	}
	defer rows.Close()
	held.Secrets = nil
	for rows.Next() {
		var name, purpose string
		if err := rows.Scan(&name, &purpose); err != nil {
			return fmt.Errorf("read skill %s secrets: %w", held.Name, err)
		}
		if held.Secrets == nil {
			held.Secrets = map[string]string{}
		}
		held.Secrets[name] = purpose
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read skill %s secrets: %w", held.Name, err)
	}
	return nil
}

func (p *Postgres) fillFiles(ctx context.Context, held *Imported) error {
	rows, err := p.pool.Query(ctx, `
		select path, body, executable from skill_files where name = $1 and version = $2 order by path`,
		held.Name, held.Version)
	if err != nil {
		return fmt.Errorf("read skill %s files: %w", held.Name, err)
	}
	defer rows.Close()
	held.Files = nil
	for rows.Next() {
		var file skill.File
		if err := rows.Scan(&file.Path, &file.Body, &file.Executable); err != nil {
			return fmt.Errorf("read skill %s files: %w", held.Name, err)
		}
		held.Files = append(held.Files, file)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read skill %s files: %w", held.Name, err)
	}
	return nil
}

// textArray is a list on its way into a text[] column that is declared not null.
//
// A nil slice arrives as an explicit NULL rather than as an absent value, so the column's default of
// '{}' never applies and the insert is refused. Every skill shipped until now declared at least one
// binary, so a skill that needs nothing from the sandbox was the first thing to hit it, and it hit it
// on a real crew rather than in a test.
func textArray(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
