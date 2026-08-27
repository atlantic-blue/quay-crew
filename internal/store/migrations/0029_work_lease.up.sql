-- Who holds a piece of work, and until when.
--
-- A controller claims work by writing these two where the lease is free or expired, in the same
-- statement, so two controllers cannot both win. That is the compare and set a log cannot give.
--
-- The lease is what makes a controller disposable. Kill the one that started a piece of work and the
-- task keeps running, because the sandbox belongs to the control plane rather than to the
-- controller. The lease expires, another controller reads the task row, and takes the answer that
-- landed rather than sending a second task for it. Work is paid for, so a second task is a second
-- bill.
--
-- Both are the only status fields a reader should ignore: they say who is holding the work, not
-- what came of it.
alter table work add column if not exists lease_owner text not null default '';
alter table work add column if not exists lease_until timestamptz;

-- The recovery query: work that is running whose lease has run out, oldest first. A crew with a
-- thousand finished pieces of work and one abandoned does one row of work per tick.
create index if not exists work_lease_idx on work (phase, lease_until);
