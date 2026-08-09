-- The shape 0011 left the table in. The rows are gone: what a workspace worked in was operator
-- configuration, and rolling back re-creates the place for it, not the knowledge.
create table if not exists workspace_repositories (
    workspace text        not null references workspaces (id),
    name      text        not null,
    remote    text        not null,
    added_at  timestamptz not null default now(),
    primary key (workspace, name)
);
