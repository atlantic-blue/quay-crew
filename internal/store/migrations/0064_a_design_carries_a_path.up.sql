-- The steps a design was broken into: one row per step of one project's path.
--
-- A separate table from project_designs because a design is one document and a path is a list. The
-- primary key is (project, number), which is also the only ordinary read there is: one project's
-- path, in number order.
--
-- The columns a later migration adds sit beside these. What a reader wants that is not here is
-- derived from the row rather than stored: a step waiting for a restatement, a step waiting for the
-- operator, and a step building are all the state word and one other column.
create table if not exists project_steps (
    project        text        not null references projects (id) on delete cascade,
    -- Where in the path, counting from one. Numbers need not run without gaps, so a path may go
    -- 1, 2, 5.
    number         integer     not null,
    title          text        not null,
    intention      text        not null default '',
    -- The files this step writes, one per line. The take reads it line by line, so a file this
    -- field does not name is a file the collision check cannot see.
    touches        text        not null default '',
    proof          text        not null default '',
    -- The exact name of the scenario that proves it, as the feature file writes it. This is what
    -- krewe runs, so it is the name itself and never a description of it.
    proof_scenario text        not null default '',
    -- The step number this one waits for. Zero means nothing blocks it.
    after          integer     not null default 0,
    -- One of ready, taken, done, stopped.
    state          text        not null default 'ready',
    -- The session that took it. Not a foreign key, for the same reason written_by is not one: the
    -- session may be archived or deleted, and the record of who took the step has to survive that.
    session        text        not null default '',
    -- What somebody wrote that the step produced, or why it stopped.
    result         text        not null default '',
    taken_at       timestamptz,
    finished_at    timestamptz,
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    primary key (project, number)
);

-- Which step a session is on, which a session listing needs to draw one row. Partial, because every
-- step nobody took carries the empty string and none of them answers that question.
--
-- There is no index on state. A whole path is read by the primary key prefix and filtered in memory,
-- and an index nothing reads is a cost with no reader.
create index if not exists project_steps_session_idx on project_steps (session) where session <> '';
