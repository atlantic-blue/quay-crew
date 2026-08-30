-- Where a role's files came from, so a role nobody can go and read is visible as one.
--
-- A role is imported from a directory, and a directory is anywhere. An acceptance run of three hours
-- was driven by three roles that sat in a folder on one machine: no pull request touched them,
-- nobody reviewed them and nothing versioned them, while every listing the crew printed showed them
-- looking exactly like the roles that ship in this repository.
--
-- Five columns rather than one sentence, because each is a different fact and the operator acts on
-- them separately: a repository to open, a commit to open it at, the directory inside it, whether
-- the files were edited after that commit, and whether the commit ever left the machine.
--
-- None of this is in the fingerprint. The same bytes read out of two checkouts are one role, so a
-- second import of a version that already exists updates these columns rather than being refused:
-- committing a loose role and importing it again is how an operator clears the warning.
--
-- Every column defaults to empty, so a role imported before today keeps working and comes back
-- saying where it came from was not recorded, which is what is true of it.
alter table roles add column if not exists origin_repository text not null default '';
alter table roles add column if not exists origin_commit text not null default '';
alter table roles add column if not exists origin_path text not null default '';
alter table roles add column if not exists origin_dirty boolean not null default false;
alter table roles add column if not exists origin_unpushed boolean not null default false;
