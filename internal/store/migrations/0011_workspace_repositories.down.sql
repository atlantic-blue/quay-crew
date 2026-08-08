alter table projects add column if not exists remote text not null default '';

-- One repository per project is all the old shape held, so a workspace with several loses all but one.
-- Which one is not worth choosing carefully: going back is a deliberate act, and the rows are still in
-- workspace_repositories until this drops them.
update projects p
set remote = (
    select r.remote from workspace_repositories r
    where r.workspace = p.workspace
    order by r.added_at, r.name
    limit 1
)
where exists (select 1 from workspace_repositories r where r.workspace = p.workspace);

drop table if exists workspace_repositories;
