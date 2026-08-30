-- The word for the level above every workspace became "system", so what holds its name moves with it.
--
-- A rename rather than a new table. These four hold the operator's secrets, skills, hooks and roles,
-- and a fresh empty table beside the old one would read as a system that had lost every one of them.
alter table if exists crew_secrets rename to system_secrets;
alter table if exists crew_skills rename to system_skills;
alter table if exists crew_hooks rename to system_hooks;
alter table if exists crew_roles rename to system_roles;

-- The context held at that level is found by its scope, so a row still saying "crew" is a document
-- nothing reads, which on the screen is indistinguishable from one that was never written.
update contexts set scope = 'system' where scope = 'crew';
