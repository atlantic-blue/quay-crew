-- Reading jobs by the moment they finished, newest first.
--
-- The briefing's third block answers "what did the system produce", and that question is about when a
-- job ended rather than when it was declared. The two orders are not the same: a job declared this
-- morning can finish before one declared last week. Every read of the jobs table until now ordered
-- by created_at, so nothing indexed the other moment and the read was a sort of the whole table.
--
-- Partial, because a job that has not finished is never in this window and there is no reason to
-- carry it in the index.
create index if not exists jobs_finished_idx on jobs (finished_at desc) where finished_at is not null;
