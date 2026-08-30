-- A project says where its work lands, and what kind of repository that is.
--
-- The crew held no record that a repository was this project's, so a job about to run could not be
-- checked for having somewhere to push, and every job had to be told the address again. A project is
-- the body of work; the repository is where that body of work goes.
--
-- visibility is "public" or "private", and it is a cost fact rather than a permission: a pipeline's
-- minutes are free on a public repository and metered on a private one. It is what the operator
-- declared, not what the forge says.
--
-- Both default to the empty string rather than null, the way every other text column on this table
-- does: a reader that has to tell null from empty is a reader with two cases where there is one.
alter table projects add column if not exists repository text not null default '';
alter table projects add column if not exists visibility text not null default '';
