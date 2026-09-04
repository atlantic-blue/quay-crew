-- What a project is for, and what was designed for it.
--
-- One row per project, and the row appears when somebody sets a brief or a design body. A project
-- with no row has no design, which is the normal state and not an error.
--
-- A separate table rather than columns on `projects`, for two reasons. Every project listing reads
-- `projects`, and a design body is the largest text in the system. The row also carries its own
-- timestamps and its own writer.
create table if not exists project_designs (
    project    text        primary key references projects (id) on delete cascade,
    brief      text        not null default '',
    body       text        not null default '',
    -- The session that last wrote `body`, empty when the operator wrote it. Not a foreign key: the
    -- session may be archived or deleted, and the record of who wrote the design must survive that.
    written_by text        not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
