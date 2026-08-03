-- A project sits between a workspace and its threads: a body of work with its own context.
--
-- Existing sessions belong to a workspace and to no project, and they hold conversation handles that
-- cannot be recreated. So this gives every workspace a project to hold what it already has, rather
-- than requiring anyone to choose or dropping the rows.

create table if not exists projects (
    id         text primary key,
    workspace  text        not null references workspaces (id),
    name       text        not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    deleted_at timestamptz
);

create index if not exists projects_workspace_idx on projects (workspace) where deleted_at is null;

-- One project per existing workspace, to adopt its sessions. The id is derived from the workspace id
-- so this migration is deterministic: run it twice on the same database and it lands on the same row.
insert into projects (id, workspace, name)
select 'p' || substr(id, 2), id, 'default'
from workspaces
on conflict (id) do nothing;

alter table sessions add column if not exists project text references projects (id);

update sessions
set project = 'p' || substr(workspace, 2)
where project is null;

alter table sessions alter column project set not null;

-- A thread is unique within its project now, not within the workspace. Two projects in one workspace
-- may legitimately carry the same thread identifier.
--
-- The old constraint is still called sessions_project_thread_id_key: migration 0002 renamed the
-- column from project to workspace, and Postgres does not rename a constraint with its column. Both
-- names are dropped so this works whether the database came through 0002 or was built fresh.
alter table sessions drop constraint if exists sessions_project_thread_id_key;
alter table sessions drop constraint if exists sessions_workspace_thread_id_key;
create unique index if not exists sessions_project_thread_idx on sessions (project, thread_id);
create index if not exists sessions_project_idx on sessions (project);
