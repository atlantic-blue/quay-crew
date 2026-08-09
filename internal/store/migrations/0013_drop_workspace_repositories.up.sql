-- A skill is a text file the session follows, and the repository machinery grew an API instead:
-- records on the workspace, a clone at sandbox birth, a worktree per session. A session clones in
-- conversation now, following the git skill, so nothing reads this table any more.
--
-- Dropped rather than kept around empty: a table nothing writes is a question every later migration
-- has to answer.
drop table if exists workspace_repositories;
