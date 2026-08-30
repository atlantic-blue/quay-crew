-- Back to the word the level used to take. Nothing is lost either way: both directions rename the
-- table the rows are already in.
alter table if exists system_secrets rename to crew_secrets;
alter table if exists system_skills rename to crew_skills;
alter table if exists system_hooks rename to crew_hooks;
alter table if exists system_roles rename to crew_roles;

update contexts set scope = 'crew' where scope = 'system';
