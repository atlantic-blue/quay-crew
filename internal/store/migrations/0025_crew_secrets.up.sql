-- The credentials every workspace needs, held once by the crew.
--
-- A subscription token, a forge token, a credential file: each one was set again for every workspace,
-- and a workspace made tomorrow started with none of them. This is the same level the crew's skills,
-- hooks and roles are attached at, reached the same way.
--
-- A separate table rather than a reserved workspace identifier in `secrets`, because the crew is not
-- a workspace: it holds no projects, no sessions and no channels, and a row pretending otherwise
-- would be one every query about workspaces had to remember to exclude.
--
-- Sealed with the same key `secrets` uses, for the same reason: a database dump is a thing people
-- paste into messages, and a token in one is a token somebody else has.
--
-- One row per name. A workspace's own secret stays separate and stays narrower, and wins on a name.
create table if not exists crew_secrets (
    name       text        not null,
    sealed     bytea       not null,
    projection text        not null default 'env',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (name)
);
