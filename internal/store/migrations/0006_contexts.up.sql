-- The files the model reads, kept where every other thing the crew knows is kept.
--
-- They lived only as files on the host, which works on one machine and nowhere else: a pod has no
-- host directory to bind mount, and an API cannot edit a file on somebody's laptop. The file inside a
-- sandbox becomes a rendering of this, written when the sandbox is made, and read back when something
-- inside has changed it.
--
-- scope is "crew", "workspace" or "project"; owner is the workspace or project it belongs to, and
-- empty for the crew, which is why the key is both columns rather than a reference.
create table if not exists contexts (
    scope      text        not null,
    owner      text        not null,
    body       text        not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (scope, owner)
);
