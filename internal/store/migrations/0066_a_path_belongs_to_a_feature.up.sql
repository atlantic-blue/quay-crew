-- A path belongs to a feature, so the step table is keyed by the feature and not by the project.
--
-- A project delivers several features at once, and each one has a path of its own. Keyed by the
-- project, a second path replaced the first, and two features could not each hold a step 3.
--
-- The rows that already exist move with the key. Every project holding steps gets one feature, and
-- its steps point at that feature, so no path is lost to the re-keying. A project holding no step
-- gets none: a feature nobody asked for, over a project with no path, is a row that says nothing.
--
-- The identifier of that feature is derived from the project rather than drawn at random, so the
-- down migration can name exactly the features this one made.
alter table project_steps rename to feature_steps;
alter index if exists project_steps_session_idx rename to feature_steps_session_idx;

insert into features (id, project, number, title)
select substr(md5(p.id || ':the path this project already held'), 1, 24),
       p.id,
       -- Number 1 for a project that holds no feature, which is every project written before
       -- features existed. A project that added one already keeps it, and this one takes the next
       -- number, because the number is unique inside the project.
       coalesce((select max(f.number) from features f where f.project = p.id), 0) + 1,
       p.name
from projects p
where exists (select 1 from feature_steps s where s.project = p.id);

alter table feature_steps add column feature text references features (id) on delete cascade;

update feature_steps s
set feature = substr(md5(s.project || ':the path this project already held'), 1, 24);

alter table feature_steps alter column feature set not null;
-- Dropping the column drops the primary key it is half of, so the new key is added after it.
alter table feature_steps drop column project;
alter table feature_steps add primary key (feature, number);
