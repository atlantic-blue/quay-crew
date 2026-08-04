-- The projection of the turn stream: what happened in a session, in order.
--
-- This is a read model, not the source of truth. The log is the write side, so this table can be
-- dropped and rebuilt by consuming the stream again from the beginning, which is the whole reason
-- the log carries enough to rebuild a conversation without reading anything else.
--
-- The id comes from the event rather than from here. Delivery is at least once, so the same record
-- arrives more than once and the primary key is what makes writing it twice harmless.
create table if not exists turns (
    id          text        primary key,
    session     text        not null,
    workspace   text        not null,
    project     text        not null,
    thread_id   text        not null default '',
    prompt      text        not null default '',
    reply       text        not null default '',
    status      text        not null default '',
    failure     text        not null default '',
    occurred_at timestamptz not null,
    created_at  timestamptz not null default now()
);

-- A session's history is read by session, newest first, which is the only query this table answers.
create index if not exists turns_session_idx on turns (session, occurred_at desc);
