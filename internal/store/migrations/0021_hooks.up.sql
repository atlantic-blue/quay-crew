-- Hooks the crew holds, one row per revision.
--
-- The same shape as skills, because a hook is the same kind of thing: files somebody wrote, imported
-- into the crew, pinned to a version, attached at a level. A session is pinned to the revision it
-- started with, so editing a hook never changes a constraint a session is already running under.
--
-- What it is not is a skill. A hook is never read by the model, so there is no brief here.
create table if not exists hooks (
    name        text        not null,
    version     integer     not null,
    summary     text        not null,
    binaries    text[]      not null default '{}',
    -- fingerprint is what this revision is, over every file. Importing the same version twice is only
    -- harmless when it is the same hook, and this is how that is answered without reading the files
    -- back.
    fingerprint text        not null,
    imported_at timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    primary key (name, version)
);

-- What a hook fires on. A hook with no rows here fires on nothing, which is refused before it
-- reaches the store, because a constraint that is never called cannot be told from one that approves
-- of everything.
create table if not exists hook_events (
    name            text    not null,
    version         integer not null,
    -- ordinal keeps the order the bindings were written in. A settings file is rendered from these,
    -- and one that reorders itself between reads is a diff nobody can review.
    ordinal         integer not null,
    event           text    not null,
    -- matcher is which tools this fires for, empty for every tool. Only PreToolUse and PostToolUse
    -- fire per tool; anywhere else a matcher is refused before it gets here.
    matcher         text    not null default '',
    -- entry is the executable to run, relative to the hook's own directory.
    entry           text    not null,
    -- timeout_seconds is how long the runtime waits. Zero means the runtime's own default.
    timeout_seconds integer not null default 0,
    primary key (name, version, ordinal),
    foreign key (name, version) references hooks (name, version) on delete cascade
);

-- The secrets a hook names, never their values. A value in a hook file is a value in a git
-- repository, so the crew binds them from its own sealed store at sandbox creation.
create table if not exists hook_secrets (
    name    text    not null,
    version integer not null,
    secret  text    not null,
    -- purpose says which credential to go and get, and how to set it, for whoever reads a refusal.
    purpose text    not null,
    primary key (name, version, secret),
    foreign key (name, version) references hooks (name, version) on delete cascade
);

-- Every file of the hook's directory, which is what gets mounted into a sandbox.
create table if not exists hook_files (
    name       text    not null,
    version    integer not null,
    path       text    not null,
    body       bytea   not null,
    -- executable is carried because an entry point that arrives without its bit cannot run, and the
    -- failure surfaces inside a container with nothing pointing back at the import.
    executable boolean not null default false,
    primary key (name, version, path),
    foreign key (name, version) references hooks (name, version) on delete cascade
);

-- Which hooks a workspace holds, at the version it pinned when it was attached.
--
-- One row per workspace and hook: a workspace holds a hook once, at one version. Attaching again is
-- how a workspace moves to a newer revision.
create table if not exists workspace_hooks (
    workspace   text        not null references workspaces (id),
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (workspace, name),
    foreign key (name, version) references hooks (name, version)
);

create index if not exists workspace_hooks_name_idx on workspace_hooks (name);

-- Which hooks the whole crew holds. A workspace's own attachment stays separate, stays narrower, and
-- wins on a name.
create table if not exists crew_hooks (
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (name),
    foreign key (name, version) references hooks (name, version)
);
