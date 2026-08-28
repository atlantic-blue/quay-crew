-- A trigger is something that happened, waiting to start a flow run. Slice 9 of the orchestration
-- delivery, section 14 of docs/ORCHESTRATION.md, and the first slice of quay-crew#399.
--
-- Its own table, beside work_events rather than inside it. The two look alike on purpose and merging
-- them would break both: an audit record is never claimed, so marking one consumed rewrites the
-- history, and a trigger must be claimed exactly once, because the claim is what stops two pollers
-- starting two runs from one event. They are also read by different queries. This one reads the few
-- rows that are pending; the audit read is by work and by time, and one table would make this poll
-- scan the history forever.
--
-- The row is written in the transaction of whatever caused it, so there is no gap for a crash to
-- hide in, and the run only ever starts from the row. Nothing writes one from outside this process
-- yet: the ingress that reads the event log is slice 3 of that issue, and QC_KAFKA_SEEDS is
-- untouched by this.
create table if not exists pending_triggers (
    id          text        primary key,
    -- The flow to run, and where to run it. The row names the graph rather than a node, because
    -- whoever raises a trigger knows what should happen and not where in a graph it happens.
    graph_name  text        not null,
    workspace   text        not null references workspaces (id),
    project     text        not null references projects (id),
    -- What the trigger carried. It becomes the run's opening state, which is what a prompt template
    -- reads with {{key}}.
    payload     jsonb       not null default '{}',
    -- What raised this, for a reader. The crew does not act on it.
    source      text        not null default '',
    -- The piece of work that caused this, where one did. The run's own work hangs under it, so a
    -- flow triggered by work that finished is bounded by the same depth limit and the same tree
    -- budget as everything else in that tree.
    cause       text        references work (id),
    -- pending, started or failed. Both of the last two are ends of the road: a trigger is acted on
    -- once, and a trigger that started nothing keeps the sentence saying why on the row, because a
    -- trigger that quietly did nothing is the failure this column exists to make visible.
    status      text        not null default 'pending',
    run         text        references flow_runs (id),
    reason      text        not null default '',
    -- The same lease discipline the work controller holds a piece of work under. A poller claims the
    -- row before it starts the run, and a poller that dies inside that window leaves a claim that
    -- runs out, so another one picks the trigger up.
    lease_owner text        not null default '',
    lease_until timestamptz,
    attempts    int         not null default 0,
    raised_at   timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);

-- The poller's own query: the few rows nothing has started a run from and nobody is holding. A crew
-- with a million triggers behind it does the work of the ones that arrived since the last tick.
create index if not exists pending_triggers_waiting_idx
    on pending_triggers (raised_at)
    where status = 'pending';
