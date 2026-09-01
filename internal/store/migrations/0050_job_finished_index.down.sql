-- Reverses 0050. Nothing but the read order depends on it, so dropping it costs a slower briefing
-- and no behaviour.
drop index if exists jobs_finished_idx;
