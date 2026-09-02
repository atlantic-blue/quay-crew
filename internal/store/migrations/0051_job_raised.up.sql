-- A job that waits for a person tells somebody, and the moment it did is on the row.
--
-- The failure it answers: on 1 September 2026 four jobs stopped for a person and nothing told him.
-- The oldest waited more than one hour, and he found out because he asked what the state was. The
-- transition already wrote job.asked to the event log, and nothing read it.
--
-- raised_at is when the first surface named this job as waiting. It is the guard as well as the
-- record: a surface raises a job only where this is null, so the console redrawing every three
-- seconds writes one job.raised for each wait rather than one for each poll. A wait that starts
-- again clears it.
--
-- asked_at is when the question went on the row. The gap between the two is the time a person spent
-- not knowing, which is the number this work is judged on, and it cannot be read off updated_at:
-- anything that touches the row afterwards moves that.
--
-- Null rather than the zero moment, because a job that never asked and one that asked at the start
-- of the epoch must not read the same.
alter table jobs add column if not exists asked_at timestamptz;
alter table jobs add column if not exists raised_at timestamptz;

-- How long a job may wait for a person in this workspace before the telling names the age beside it.
-- Zero takes the system's own, which is 15 minutes and is a guess: see job.DefaultWaiting.
alter table workspace_limits add column if not exists waiting_seconds integer not null default 0;
