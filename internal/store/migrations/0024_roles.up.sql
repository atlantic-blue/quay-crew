-- Roles the crew holds, one row per revision.
--
-- The same shape as skills and hooks, because a role is the same kind of thing: files somebody
-- wrote, imported into the crew, pinned to a version, attached at a level. A session is pinned to
-- the revision it started with, so editing a role never changes how a session already running as it
-- was told to work.
--
-- What a role is not is a skill. A skill is a capability every session in a workspace may reach for;
-- a role is the whole instruction of one session, and the material that session may receive.
create table if not exists roles (
    name        text        not null,
    version     integer     not null,
    summary     text        not null,
    -- model is which model a session running as this role uses, as a tier or a full name. A column
    -- rather than crew wide configuration, because what a role costs is part of what it is.
    model       text        not null,
    -- receives is the material this role is given. An empty list is refused before it reaches here:
    -- a role is its boundary, so a role that declares no boundary is not a role.
    receives    text[]      not null default '{}',
    brief       text        not null,
    -- fingerprint is what this revision is. Importing the same version twice is only harmless when
    -- it is the same role, and this is how that is answered without comparing every column.
    fingerprint text        not null,
    imported_at timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    primary key (name, version)
);

-- Which roles a workspace holds, at the version it pinned when it was attached.
--
-- One row per workspace and role: a workspace holds a role once, at one version. Attaching again is
-- how a workspace moves to a newer revision.
create table if not exists workspace_roles (
    workspace   text        not null references workspaces (id),
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (workspace, name),
    foreign key (name, version) references roles (name, version)
);

create index if not exists workspace_roles_name_idx on workspace_roles (name);

-- Which roles the whole crew holds. A workspace's own attachment stays separate, stays narrower, and
-- wins on a name.
create table if not exists crew_roles (
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (name),
    foreign key (name, version) references roles (name, version)
);
