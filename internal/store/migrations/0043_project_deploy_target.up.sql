-- Where a project ships: the account, the region inside it, and the identity a pipeline assumes.
--
-- A workspace is a bag of secrets and a project is a name, so this lived in one person's memory and
-- was said out loud when somebody asked. The cost of getting it wrong is a tree of jobs that writes
-- correct infrastructure for an account it can never reach.
--
-- Three columns rather than one document, because each is asked for on its own: which account, which
-- region, which role. All three default to the empty string, the way every other text column on this
-- table does, and all three empty is a project that has not said. The control plane refuses half of
-- one, so a row carrying two of the three cannot be written through it.
alter table projects add column if not exists deploy_account text not null default '';
alter table projects add column if not exists deploy_region text not null default '';
alter table projects add column if not exists deploy_identity text not null default '';
