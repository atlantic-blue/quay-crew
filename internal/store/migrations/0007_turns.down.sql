-- Reversing this loses nothing that cannot be rebuilt: the turns are still on the event log, and the
-- projection reads them back from the beginning.
drop table if exists turns;
