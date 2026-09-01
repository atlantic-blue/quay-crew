-- The controller reads the sessions it can act on in two queries rather than one, so that neither
-- starves the other, and it asks on every tick whether the system is doing anything at all.
--
-- What went wrong. The fourth query read every settled session, ordered by updated_at, capped at
-- twenty. A reclaimed session stays settled: with no archive time set nothing moves it, ever. Its
-- updated_at is the moment it was reclaimed, so it sorts ahead of a sandbox that has only been idle
-- for an hour. Once twenty reclaimed rows sat at the front, the batch was all of them and the
-- reclaim never reached a container again. Twelve sandboxes then held a whole machine's processors
-- while five jobs waited for room and nothing ran. See issue 575.
--
-- No column changes here. The split is in the queries, and these are the two indexes that keep each
-- of them one lookup.

-- The archive query, which measures against the reclaim stamp rather than against updated_at.
create index if not exists sessions_reclaimed_idx
    on sessions (status, reclaimed_at) where archived_at is null;

-- The fifth comparison, in one lookup: the jobs the machine turned away. Only the system writes a
-- reason on a pending job, and only when it holds the job back, so the reason is the whole condition
-- and the index holds nothing else.
create index if not exists jobs_turned_away_idx
    on jobs (created_at, id) where phase = 'pending' and reason <> '';
