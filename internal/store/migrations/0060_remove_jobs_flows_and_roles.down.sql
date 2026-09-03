-- There is no way back. The tables held the job subsystem, flows and roles, and the code that wrote
-- and read every one of them was removed in the same change.
--
-- Recreating the tables empty would give a system that answers every read and holds nothing, which is
-- the failure this whole removal is about. A rollback is a checkout of the commit before it.
--
-- The column is the one thing that can go back, so it does.
alter table sessions add column if not exists role text not null default '';
