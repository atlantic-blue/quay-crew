-- The flow engine's substrate, decided 9 August 2026: a run and its transitions are rows written in
-- one transaction, so reconstructable is a guarantee rather than a sentence, and there is no gap
-- for a dropped publish to hide in.

-- A graph at a version. Immutable once imported: a run is pinned to the version it started with,
-- and a version that can change is not a pin.
create table if not exists flow_graphs (
    name        text        not null,
    version     int         not null,
    definition  text        not null,
    imported_at timestamptz not null default now(),
    primary key (name, version)
);

create table if not exists flow_runs (
    id            text        primary key,
    workspace     text        not null references workspaces (id),
    project       text        not null references projects (id),
    graph_name    text        not null,
    graph_version int         not null,
    node          text        not null,
    status        text        not null,
    state         jsonb       not null default '{}',
    attempts      jsonb       not null default '{}',
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    foreign key (graph_name, graph_version) references flow_graphs (name, version)
);

-- One row per movement of a run, appended in the same transaction as the run's new position. This
-- is the audit record and the replay record, transactional by construction.
create table if not exists flow_run_events (
    run         text        not null references flow_runs (id),
    seq         int         not null,
    event       text        not null,
    node        text        not null,
    occurred_at timestamptz not null default now(),
    primary key (run, seq)
);

-- The idempotency ledger: one row per dispatch, keyed by run, node and attempt, inserted in the
-- same transaction as the transition that asked for it. A duplicate here refuses the whole
-- movement, which is the compare and set a log cannot do, and it is what keeps the same turn from
-- ever being sent, and paid for, twice.
create table if not exists flow_dispatches (
    run     text not null references flow_runs (id),
    node    text not null,
    attempt int  not null,
    primary key (run, node, attempt)
);
