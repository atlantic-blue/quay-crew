-- The trace a record belongs to, so history and traces can be joined.
--
-- A log line links to its trace and a trace links to its log lines, but the durable record of what
-- the crew did linked to neither. Weeks later the logs are gone and the row is all that is left,
-- with no way back. See issue 346.
--
-- The crew's correlation identifier is the trace identifier rather than a second value beside it, so
-- this is the same identifier that is already on every log line. One value joins a piece of work,
-- its children, the tasks they ran, the spans around them and the lines written under them.
alter table tasks add column if not exists trace_id text not null default '';

-- On a piece of work the trace is minted at the root and inherited unchanged by every descendant, so
-- one trace covers a whole tree. parent_span_id is the span the caller was inside when it declared
-- this work, and it is empty for a root nothing was tracing.
--
-- Both are columns rather than something a process holds. That is what makes a trace survive the
-- controller that started the work: the context is in the declaration, the way a wait is a column
-- rather than a timer.
alter table work add column if not exists trace_id text not null default '';
alter table work add column if not exists parent_span_id text not null default '';

-- And on each record of a movement, so a reader holding one event reaches the trace it happened in
-- without reading the work row first.
alter table work_events add column if not exists trace_id text not null default '';
