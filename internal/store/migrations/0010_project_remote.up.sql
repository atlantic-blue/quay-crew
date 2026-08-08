-- The repository a project's sessions work in.
--
-- A session's working directory starts empty, so a git skill opened onto bare ground: the crew could
-- describe how to commit and there was nothing to commit in. This is where the code comes from.
--
-- On the project rather than the workspace, because a body of work is one repository and a workspace
-- holds several bodies of work. Empty is the normal state: most projects have no code in them.
alter table projects add column if not exists remote text not null default '';
