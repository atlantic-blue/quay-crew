-- What a workspace lets its sessions declare.
--
-- A role carries the grant and the workspace carries the ceiling, and they mean different things: a
-- role says what a session may do, and this says how much of it. The effective capability is the
-- intersection. A role alone would grant the same power in every workspace it is attached to,
-- including the ones the operator never thought about.
--
-- The workspace is already the unit of tenancy: secrets, skills, channels and isolation are all
-- scoped here, and a quota is a tenancy concern.
--
-- A workspace with no row here takes the defaults below, and the default for max_depth is zero. No
-- session may declare work until an operator raises it, per workspace, deliberately. Default deny.
create table if not exists workspace_limits (
    workspace     text        primary key references workspaces (id),
    -- How deep the tree of work may go. Zero means a session may declare none.
    max_depth     int         not null default 0,
    -- How many pieces of work may run at once here. Zero is unset, and the crew ships it unset
    -- because nothing has measured the number at which a host's memory pressure appears.
    max_running   int         not null default 0,
    -- What a tree may spend when its root declares no budget. Zero is unset for the same reason.
    budget_tokens bigint      not null default 0,
    -- How long a controller holds a piece of work here. Zero takes the crew's measured default.
    lease_seconds int         not null default 0,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);
