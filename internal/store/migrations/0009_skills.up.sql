-- Skills: a capability a session can be given, and which workspaces hold it.
--
-- A skill is authored as a directory in a git repository, which is what makes it reviewable and
-- versioned, and imported here. The files travel rather than the path: the control plane runs in a
-- container and a directory on the operator's machine means nothing to it, and a crew on a pod has no
-- host directory to go back to for whatever it did not copy.
--
-- The key is name and version rather than an identifier, because a session pins the version it holds
-- and both halves are needed to find it. A skill edited in its repository is a new version, so it
-- cannot change under a session that is already using it.
create table if not exists skills (
    name        text        not null,
    version     integer     not null,
    summary     text        not null,
    binaries    text[]      not null default '{}',
    brief       text        not null,
    -- fingerprint is what this revision is, over every file. Importing the same version twice is only
    -- harmless when it is the same skill, and this is how that is answered without reading the files
    -- back.
    fingerprint text        not null,
    imported_at timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    primary key (name, version)
);

-- The secrets a skill names, never their values. A value in a skill file is a value in a git
-- repository, so the crew binds them from its own sealed store at sandbox creation.
create table if not exists skill_secrets (
    name    text    not null,
    version integer not null,
    secret  text    not null,
    -- purpose says which credential to go and get, and how to set it, for whoever reads a refusal.
    purpose text    not null,
    primary key (name, version, secret),
    foreign key (name, version) references skills (name, version) on delete cascade
);

-- Every file of the skill's directory, which is what gets mounted into a sandbox.
create table if not exists skill_files (
    name       text    not null,
    version    integer not null,
    path       text    not null,
    body       bytea   not null,
    -- executable is carried because a setup script that arrives without its bit cannot run, and the
    -- failure surfaces inside a container with nothing pointing back at the import.
    executable boolean not null default false,
    primary key (name, version, path),
    foreign key (name, version) references skills (name, version) on delete cascade
);

-- Which skills a workspace holds, at the version it pinned when it was attached.
--
-- One row per workspace and skill: a workspace holds a skill once, at one version. Attaching again is
-- how a workspace moves to a newer revision.
create table if not exists workspace_skills (
    workspace   text        not null references workspaces (id),
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (workspace, name),
    foreign key (name, version) references skills (name, version)
);

create index if not exists workspace_skills_name_idx on workspace_skills (name);
