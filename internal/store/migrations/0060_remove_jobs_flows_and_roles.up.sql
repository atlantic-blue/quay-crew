-- The job subsystem, flows and roles are removed from the product, so their tables go with the code.
--
-- What is left is a session running a task in its own sandbox, with its hooks and its skills. Nothing
-- orchestrates above a session any more, so nothing reads these rows.
--
-- Dropped rather than left in place. A table nobody writes is a table somebody reads a year later and
-- believes, and the pull requests and the commits this work produced are in git, which is where the
-- record of it belongs.

-- The job tree, its runs, and everything hanging off a job. Every drop is a cascade: a child
-- references its parent, and listing the children by name only works while the list is complete.
-- One table left off refuses the whole migration, and the code that read any of them is gone.
drop table if exists executions cascade;
drop table if exists job_attempts cascade;
drop table if exists job_handoffs cascade;
drop table if exists job_questions cascade;
drop table if exists job_steers cascade;
drop table if exists job_steps cascade;
drop table if exists jobs cascade;
drop table if exists job_events cascade;

-- Flows: the graphs, the runs of them, and the queue of things waiting to start one.
drop table if exists flow_run_events cascade;
drop table if exists flow_dispatches cascade;
drop table if exists flow_schedules cascade;
drop table if exists flow_runs cascade;
drop table if exists flow_graphs cascade;
drop table if exists pending_triggers cascade;

-- Roles, and the two levels a role was attached at.
drop table if exists workspace_roles cascade;
drop table if exists crew_roles cascade;
drop table if exists roles cascade;

-- What a workspace let its jobs declare. Nothing declares anything now.
drop table if exists workspace_limits cascade;

-- A session no longer runs as a role.
alter table sessions drop column if exists role;
