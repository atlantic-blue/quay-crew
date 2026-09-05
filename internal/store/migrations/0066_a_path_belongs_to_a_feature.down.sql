-- This does not fully reverse the up migration, and what it cannot put back is named here because
-- the loss is silent otherwise.
--
-- A feature the operator added after the up migration ran is not one this migration made, so it is
-- left standing and its steps move to the project that holds it. The steps are kept; which feature
-- they belonged to is not, and it exists nowhere else.
--
-- The old key was (project, number), so two features of one project each holding a step 3 cannot
-- both come back. This refuses on the primary key rather than dropping one of them, because a step
-- discarded to make a key fit is work nobody can see going.
alter table feature_steps add column project text references projects (id) on delete cascade;

update feature_steps s set project = f.project from features f where f.id = s.feature;

alter table feature_steps alter column project set not null;
-- Dropping the column drops the primary key it is half of, so the old key is added after it.
alter table feature_steps drop column feature;
alter table feature_steps add primary key (project, number);

alter index if exists feature_steps_session_idx rename to project_steps_session_idx;
alter table feature_steps rename to project_steps;

-- The features the up migration made, and only those: their identifier is derived from the project.
-- The steps stopped pointing at them above, so nothing cascades from this.
delete from features f
where f.id = substr(md5(f.project || ':the path this project already held'), 1, 24);
