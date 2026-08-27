-- Work is declared intent, kept as a row so it outlives the caller that asked for it.
--
-- A caller writes a piece of work and a controller makes reality match it. The intent is not a list
-- held in a process: kill the caller, kill the crew, and the row is still here tomorrow.
--
-- The declared fields are what the caller wrote. The status fields are what a controller writes and
-- nobody else. They are one row rather than two because a reader asking "where is this" reads one
-- record, and because a status that can be written apart from its declaration is a status that can
-- describe a declaration that no longer exists.
create table if not exists work (
    id              text        primary key,
    workspace       text        not null references workspaces (id),
    project         text        not null references projects (id),

    -- What the caller declared.
    title           text        not null,
    brief           text        not null,
    role            text        not null default '',
    -- The version of the role attached at the moment of the write. A piece of work is pinned the way
    -- a run pins its graph, so editing a role cannot change work that is already declared.
    role_version    int         not null default 0,
    mode            text        not null default '',
    expect_file     text        not null default '',
    expect_contains text        not null default '',
    -- Identifiers of other work this work waits for. The whole ordering primitive: no condition, no
    -- branch, no expression.
    after_work      text[]      not null default '{}',
    deadline        timestamptz,
    budget_tokens   bigint      not null default 0,
    labels          jsonb       not null default '{}',

    -- What the crew assigned, and the caller may not. Parent is read from the credential the caller
    -- presented, which is the only reason depth bounds anything.
    parent          text        references work (id),
    depth           int         not null default 0,
    -- Version rises on every write to a declared field, so a status can be told current from stale.
    version         int         not null default 1,

    -- What the controller writes, and nobody else.
    phase           text        not null default 'pending',
    session         text        not null default '',
    attempts        int         not null default 0,
    -- The read path: an answer a caller reads as a value rather than as a transcript.
    answer          text        not null default '',
    reason          text        not null default '',
    question        text        not null default '',
    spent_tokens    bigint      not null default 0,
    observed_version int        not null default 0,

    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    started_at      timestamptz,
    finished_at     timestamptz
);

-- The three questions a reader asks. What is in this project, newest first, which is the listing.
-- What is under this piece of work, which is how a tree is read. What is still open across the crew,
-- which is what a controller will scan.
create index if not exists work_project_idx on work (project, created_at desc);
create index if not exists work_parent_idx on work (parent);
create index if not exists work_phase_idx on work (phase, created_at);

-- One row per thing that happened to a piece of work, written in the same transaction as the row it
-- describes. The store is the truth and any export follows it, so a crew with no broker at all keeps
-- the whole record.
--
-- `kind` names something that happened, in the past tense. A phase, "pending" or "running", is what
-- the work's own row says now and is the fold of these, so it is never a kind.
create table if not exists work_events (
    id          text        primary key,
    kind        text        not null,
    work        text        not null references work (id),
    workspace   text        not null,
    project     text        not null,
    parent      text        not null default '',
    depth       int         not null default 0,
    detail      text        not null default '',
    occurred_at timestamptz not null,
    created_at  timestamptz not null default now()
);

-- One piece of work's own history, and the whole crew's most recent.
create index if not exists work_events_work_idx on work_events (work, occurred_at);
create index if not exists work_events_recent_idx on work_events (occurred_at desc);
