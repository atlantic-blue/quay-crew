-- Which graphs the crew starts on its own, and where.
--
-- A schedule is a graph plus a project, because a run needs a project to dispatch into and one
-- graph may be scheduled in several. next_at is when the crew should start the next run, kept as a
-- column for the same reason a wait is: a process holding timers forgets them all when it restarts.
create table if not exists flow_schedules (
    graph_name text        not null,
    project    text        not null references projects (id),
    every_ms   bigint      not null,
    next_at    timestamptz not null,
    created_at timestamptz not null default now(),
    primary key (graph_name, project)
);

-- The poller asks "which schedules are due" on every tick, the same shape the waits query has.
create index if not exists flow_schedules_next_at_idx on flow_schedules (next_at);
