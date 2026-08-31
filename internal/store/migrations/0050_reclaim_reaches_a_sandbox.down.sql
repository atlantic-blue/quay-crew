-- Reverses 0050. The queries go back to reading one batch, which is the fault in issue 575.
drop index if exists jobs_turned_away_idx;
drop index if exists sessions_reclaimed_idx;
