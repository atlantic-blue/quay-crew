-- A session that drives the crew rather than doing work inside it.
--
-- It is marked rather than inferred, because what it can reach is wider than an ordinary session: the
-- control plane's own interface, and whatever host paths the operator hands it. Everything that
-- widens is gated on this column, so a session is ordinary unless somebody said otherwise.
alter table sessions add column driver boolean not null default false;

-- One driver per project. Two would each think they were the one, and the second would be reached by
-- nobody.
create unique index sessions_one_driver_per_project on sessions (project) where driver;
