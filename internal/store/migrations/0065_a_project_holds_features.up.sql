-- The narrowed parts of one project: one row per feature.
--
-- A project delivers several features at the same time, each with its own milestones, and neither
-- waits for the other. A feature is where that list lives.
--
-- The identifier is its own column rather than (project, number), because the tables underneath a
-- feature point at one value. The number is what a person types, and it is unique inside the project
-- rather than across the system.
--
-- A feature carries no design, no contracts document and no approval. Those belong to the project,
-- so a step taken in any feature reads the one approval on the project's design.
create table if not exists features (
    id         text        primary key,
    project    text        not null references projects (id) on delete cascade,
    -- Where in the project, counting from one. The server gives it: the highest number in the
    -- project plus one, read and written in one statement.
    number     integer     not null,
    title      text        not null,
    -- One line saying which part of the project this feature narrows to.
    intention  text        not null default '',
    -- One of open, done, stopped. The state says who holds the row and nothing else, which is why
    -- there are three words and not four.
    state      text        not null default 'open',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    -- A number is never reused: a feature that stopped keeps its number, so the next one is higher.
    unique (project, number)
);

-- One project's features, in number order, which is the only ordinary read there is. The primary key
-- answers the other one, which is a read by identifier.
create index if not exists features_project_idx on features (project);
