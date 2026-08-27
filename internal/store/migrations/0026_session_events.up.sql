-- What happened to a session, in order: created, started, completed, errored, stopped, archived.
--
-- The crew emitted one event, a finished task, so nothing could tell that a session was made or put
-- away, and nothing could react to a change. This is the stream a consumer subscribes to and a
-- workflow trigger matches on.
--
-- The store is the truth and the log is the export, decided 9 August: a row lands here in the same
-- breath as the thing it describes, and the export to `<workspace>.sessions` follows it. A view that
-- read the broker instead would go blank whenever the broker did.
--
-- The id is minted where the row is written and travels outward on the export, so the same record
-- has one identifier in both places. Writing it twice is harmless, which is what a caller retrying a
-- write it is unsure of needs.
--
-- `kind` names something that happened, in the past tense. A state, "idle" or "running", is what the
-- session's row says now and is the fold of these, so it is never a kind.
create table if not exists session_events (
    id          text        primary key,
    kind        text        not null,
    session     text        not null,
    workspace   text        not null,
    project     text        not null,
    handle      text        not null default '',
    detail      text        not null default '',
    occurred_at timestamptz not null,
    created_at  timestamptz not null default now()
);

-- Two queries: one session's own lifecycle, and the whole crew's most recent, which is what a view
-- of what is going on right now reads.
create index if not exists session_events_session_idx on session_events (session, occurred_at desc);
create index if not exists session_events_recent_idx on session_events (occurred_at desc);
