-- Projects, their channels, and their sessions.
--
-- A session's model_session_id is the handle to the conversation held by the model itself. Losing it
-- orphans a conversation that still exists, which is the whole reason this schema is here.

create table if not exists projects (
    id         text primary key,
    name       text        not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    deleted_at timestamptz
);

-- Reads filter on deleted_at, so index the live rows.
create index if not exists projects_live_idx on projects (id) where deleted_at is null;

create table if not exists channels (
    id         text        not null,
    project    text        not null references projects (id),
    kind       text        not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (project, id)
);

create index if not exists channels_project_idx on channels (project);

create table if not exists sessions (
    id               text primary key,
    project          text        not null references projects (id),
    thread_id        text        not null,
    status           text        not null,
    model_session_id text        not null default '',
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    unique (project, thread_id)
);

create index if not exists sessions_project_idx on sessions (project);
