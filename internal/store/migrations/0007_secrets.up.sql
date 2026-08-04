-- Per workspace credentials, so the subscription token stops being lost on every restart.
--
-- The value is stored encrypted and never in the clear: a database dump is a thing people paste into
-- messages and attach to issues, and a token in one is a token somebody else has. The key lives on the
-- host beside the data directory rather than in here, so holding this table is not enough to read it.
create table if not exists secrets (
    workspace  text        not null,
    name       text        not null,
    sealed     bytea       not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (workspace, name)
);
