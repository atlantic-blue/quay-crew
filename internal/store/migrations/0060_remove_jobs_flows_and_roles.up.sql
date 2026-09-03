-- The job subsystem, flows and roles are removed from the product, so their tables go with the code.
--
-- What is left is a session running a task in its own sandbox, with its hooks and its skills. Nothing
-- orchestrates above a session any more, so nothing reads these rows.
--
-- Dropped rather than left in place. A table nobody writes is a table somebody reads a year later and
-- believes, and the pull requests and the commits this work produced are in git, which is where the
-- record of it belongs.

-- The job tree, its runs, and everything hanging off a job. Order matters: a child references its
-- parent, so the referencing table goes first.
drop table if exists executions;
drop table if exists job_attempts;
drop table if exists job_handoffs;
drop table if exists job_questions;
drop table if exists job_steers;
drop table if exists job_steps;
drop table if exists jobs;
drop table if exists job_events;

-- Flows: the graphs, the runs of them, and the queue of things waiting to start one.
drop table if exists flow_run_events;
drop table if exists flow_dispatches;
drop table if exists flow_schedules;
drop table if exists flow_runs;
drop table if exists flow_graphs;
drop table if exists pending_triggers;

-- Roles, and the two levels a role was attached at.
drop table if exists workspace_roles;
drop table if exists crew_roles;
drop table if exists roles;

-- What a workspace let its jobs declare. Nothing declares anything now.
drop table if exists workspace_limits;

-- A session no longer runs as a role.
alter table sessions drop column if exists role;
