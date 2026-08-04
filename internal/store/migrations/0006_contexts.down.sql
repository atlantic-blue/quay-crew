-- Reversing this loses every context the crew holds. The rendered files in the data directory are
-- untouched, so nothing an agent can read disappears with it.
drop table if exists contexts;
