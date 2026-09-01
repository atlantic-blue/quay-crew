package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/jackc/pgx/v5"
)

// The durable store's hooks. The same six questions as skills, against their own tables.

// ImportHook takes a hook into the system, in one transaction, refusing a version that already exists
// carrying something different.
//
// The whole hook goes in together. A hook whose manifest landed and whose files did not is a
// constraint the system believes it has and cannot run, which is worse than not having it: a listing
// says the gate is there.
func (p *Postgres) ImportHook(ctx context.Context, imported ImportedHook) error {
	var held string
	err := p.pool.QueryRow(ctx,
		`select fingerprint from hooks where name = $1 and version = $2`,
		imported.Name, imported.Version).Scan(&held)
	switch {
	case err == nil && held == imported.Fingerprint():
		// The same hook, imported twice. Importing is how a hook arrives from a repository, and
		// doing it again after a pull that changed nothing should not be an error.
		return nil
	case err == nil:
		return fmt.Errorf("%w: %s version %d", ErrHookChanged, imported.Name, imported.Version)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read hook %s: %w", imported.Name, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("import hook %s: %w", imported.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into hooks (name, version, summary, binaries, fingerprint)
		values ($1, $2, $3, $4, $5)`,
		imported.Name, imported.Version, imported.Summary, textArray(imported.Binaries),
		imported.Fingerprint()); err != nil {
		return fmt.Errorf("import hook %s: %w", imported.Name, err)
	}
	for at, binding := range imported.Events {
		if _, err := tx.Exec(ctx, `
			insert into hook_events (name, version, ordinal, event, matcher, entry, timeout_seconds)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			imported.Name, imported.Version, at, binding.On, binding.Matcher, binding.Entry,
			binding.TimeoutSeconds); err != nil {
			return fmt.Errorf("import hook %s binding %s: %w", imported.Name, binding.On, err)
		}
	}
	for _, name := range imported.SecretNames() {
		if _, err := tx.Exec(ctx, `
			insert into hook_secrets (name, version, secret, purpose) values ($1, $2, $3, $4)`,
			imported.Name, imported.Version, name, imported.Secrets[name]); err != nil {
			return fmt.Errorf("import hook %s secret %s: %w", imported.Name, name, err)
		}
	}
	for _, file := range imported.Files {
		if _, err := tx.Exec(ctx, `
			insert into hook_files (name, version, path, body, executable) values ($1, $2, $3, $4, $5)`,
			imported.Name, imported.Version, file.Path, file.Body, file.Executable); err != nil {
			return fmt.Errorf("import hook %s file %s: %w", imported.Name, file.Path, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("import hook %s: %w", imported.Name, err)
	}
	return nil
}

// GetHook returns one revision of a hook, files included.
func (p *Postgres) GetHook(ctx context.Context, name string, version int) (ImportedHook, error) {
	held, err := p.hookRow(ctx, `where name = $1 and version = $2`, name, version)
	if err != nil {
		return ImportedHook{}, err
	}
	if err := p.fillHook(ctx, &held); err != nil {
		return ImportedHook{}, err
	}
	return held, nil
}

// ListHooks returns the newest revision of every hook, without their files.
//
// The bindings still come, because what a hook fires on is the whole of what a listing is asked. The
// files are what a listing does not need and what would make the cheapest call the most expensive.
func (p *Postgres) ListHooks(ctx context.Context) ([]ImportedHook, error) {
	out, err := p.hookRows(ctx, `
		select h.name, h.version, h.summary, h.binaries, h.imported_at
		from hooks h
		join (select name, max(version) as version from hooks group by name) newest
		  on newest.name = h.name and newest.version = h.version
		order by h.name`)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	for at := range out {
		if err := p.fillHookEvents(ctx, &out[at]); err != nil {
			return nil, err
		}
		if err := p.fillHookSecrets(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AttachHook gives a workspace a hook at the newest revision the system holds.
func (p *Postgres) AttachHook(ctx context.Context, workspace, name string) (ImportedHook, error) {
	if _, err := p.GetWorkspace(ctx, workspace); err != nil {
		return ImportedHook{}, err
	}
	newest, err := p.hookRow(ctx, `where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return ImportedHook{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into workspace_hooks (workspace, name, version) values ($1, $2, $3)
		on conflict (workspace, name) do update
		  set version = excluded.version, attached_at = now()`,
		workspace, newest.Name, newest.Version); err != nil {
		return ImportedHook{}, fmt.Errorf("attach hook %s: %w", name, err)
	}
	if err := p.fillHook(ctx, &newest); err != nil {
		return ImportedHook{}, err
	}
	return newest, nil
}

// DetachHook takes a hook away from a workspace.
func (p *Postgres) DetachHook(ctx context.Context, workspace, name string) error {
	tag, err := p.pool.Exec(ctx,
		`delete from workspace_hooks where workspace = $1 and name = $2`, workspace, name)
	if err != nil {
		return fmt.Errorf("detach hook %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WorkspaceHooks returns what a workspace holds, at the versions it pinned, files included.
func (p *Postgres) WorkspaceHooks(ctx context.Context, workspace string) ([]ImportedHook, error) {
	out, err := p.hookRows(ctx, `
		select h.name, h.version, h.summary, h.binaries, h.imported_at
		from workspace_hooks w
		join hooks h on h.name = w.name and h.version = w.version
		where w.workspace = $1
		order by h.name`, workspace)
	if err != nil {
		return nil, fmt.Errorf("list workspace hooks: %w", err)
	}
	for at := range out {
		if err := p.fillHook(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AttachSystemHook gives the whole system a hook at the newest revision it holds.
func (p *Postgres) AttachSystemHook(ctx context.Context, name string) (ImportedHook, error) {
	newest, err := p.hookRow(ctx, `where name = $1 order by version desc limit 1`, name)
	if err != nil {
		return ImportedHook{}, err
	}
	if _, err := p.pool.Exec(ctx, `
		insert into system_hooks (name, version) values ($1, $2)
		on conflict (name) do update
		  set version = excluded.version, attached_at = now()`,
		newest.Name, newest.Version); err != nil {
		return ImportedHook{}, fmt.Errorf("attach system hook %s: %w", name, err)
	}
	if err := p.fillHook(ctx, &newest); err != nil {
		return ImportedHook{}, err
	}
	return newest, nil
}

// DetachSystemHook takes a hook away from the system.
func (p *Postgres) DetachSystemHook(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `delete from system_hooks where name = $1`, name)
	if err != nil {
		return fmt.Errorf("detach system hook %s: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SystemHooks returns what the system holds, at the versions it pinned, files included.
func (p *Postgres) SystemHooks(ctx context.Context) ([]ImportedHook, error) {
	out, err := p.hookRows(ctx, `
		select h.name, h.version, h.summary, h.binaries, h.imported_at
		from system_hooks c
		join hooks h on h.name = c.name and h.version = c.version
		order by h.name`)
	if err != nil {
		return nil, fmt.Errorf("list system hooks: %w", err)
	}
	for at := range out {
		if err := p.fillHook(ctx, &out[at]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// hookRows reads hook rows, without their bindings, secrets or files.
func (p *Postgres) hookRows(ctx context.Context, query string, args ...any) ([]ImportedHook, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImportedHook
	for rows.Next() {
		var held ImportedHook
		if err := rows.Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries,
			&held.ImportedAt); err != nil {
			return nil, err
		}
		out = append(out, held)
	}
	return out, rows.Err()
}

// hookRow reads one hook's own row, without its bindings, secrets or files.
func (p *Postgres) hookRow(ctx context.Context, where string, args ...any) (ImportedHook, error) {
	var held ImportedHook
	err := p.pool.QueryRow(ctx, `
		select name, version, summary, binaries, imported_at from hooks `+where, args...).
		Scan(&held.Name, &held.Version, &held.Summary, &held.Binaries, &held.ImportedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportedHook{}, ErrNotFound
	}
	if err != nil {
		return ImportedHook{}, fmt.Errorf("read hook: %w", err)
	}
	return held, nil
}

// fillHook adds a hook's bindings, its secrets and its files.
func (p *Postgres) fillHook(ctx context.Context, held *ImportedHook) error {
	if err := p.fillHookEvents(ctx, held); err != nil {
		return err
	}
	if err := p.fillHookSecrets(ctx, held); err != nil {
		return err
	}
	return p.fillHookFiles(ctx, held)
}

// fillHookEvents reads the bindings back in the order they were written, which is what the ordinal
// column is for: a settings file is rendered from these, and one that reorders between reads is a
// diff nobody can review.
func (p *Postgres) fillHookEvents(ctx context.Context, held *ImportedHook) error {
	rows, err := p.pool.Query(ctx, `
		select event, matcher, entry, timeout_seconds from hook_events
		where name = $1 and version = $2 order by ordinal`, held.Name, held.Version)
	if err != nil {
		return fmt.Errorf("read hook %s events: %w", held.Name, err)
	}
	defer rows.Close()
	held.Events = nil
	for rows.Next() {
		var binding hook.Binding
		if err := rows.Scan(&binding.On, &binding.Matcher, &binding.Entry,
			&binding.TimeoutSeconds); err != nil {
			return fmt.Errorf("read hook %s events: %w", held.Name, err)
		}
		held.Events = append(held.Events, binding)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read hook %s events: %w", held.Name, err)
	}
	return nil
}

func (p *Postgres) fillHookSecrets(ctx context.Context, held *ImportedHook) error {
	rows, err := p.pool.Query(ctx, `
		select secret, purpose from hook_secrets where name = $1 and version = $2 order by secret`,
		held.Name, held.Version)
	if err != nil {
		return fmt.Errorf("read hook %s secrets: %w", held.Name, err)
	}
	defer rows.Close()
	held.Secrets = nil
	for rows.Next() {
		var name, purpose string
		if err := rows.Scan(&name, &purpose); err != nil {
			return fmt.Errorf("read hook %s secrets: %w", held.Name, err)
		}
		if held.Secrets == nil {
			held.Secrets = map[string]string{}
		}
		held.Secrets[name] = purpose
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read hook %s secrets: %w", held.Name, err)
	}
	return nil
}

func (p *Postgres) fillHookFiles(ctx context.Context, held *ImportedHook) error {
	rows, err := p.pool.Query(ctx, `
		select path, body, executable from hook_files where name = $1 and version = $2 order by path`,
		held.Name, held.Version)
	if err != nil {
		return fmt.Errorf("read hook %s files: %w", held.Name, err)
	}
	defer rows.Close()
	held.Files = nil
	for rows.Next() {
		var file hook.File
		if err := rows.Scan(&file.Path, &file.Body, &file.Executable); err != nil {
			return fmt.Errorf("read hook %s files: %w", held.Name, err)
		}
		held.Files = append(held.Files, file)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read hook %s files: %w", held.Name, err)
	}
	return nil
}
