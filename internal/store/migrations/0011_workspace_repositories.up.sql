-- The repositories a workspace works in, moved up from the project.
--
-- A remote sat on the project for one commit. The workspace is where it belongs: it is already where a
-- credential lives and where a skill attaches, and those are the two things a repository needs. A
-- workspace is also the level a person thinks at, and several bodies of work in one workspace almost
-- always share the same code.
--
-- Several rather than one, because a workspace routinely spans more than one repository: a service and
-- its infrastructure, or a frontend and the API behind it.
--
-- name is the directory the checkout lands in, derived from the remote when it is added, and it is what
-- a repository is removed by. Unique per workspace, so two remotes ending in the same name cannot both
-- claim one directory.
create table if not exists workspace_repositories (
    workspace text        not null references workspaces (id),
    name      text        not null,
    remote    text        not null,
    added_at  timestamptz not null default now(),
    primary key (workspace, name)
);

-- Carry over anything already set on a project. The name is derived the same way the crew derives it:
-- the last path segment without its .git. Two projects in one workspace naming the same repository
-- collapse into the one row, which is the right answer.
insert into workspace_repositories (workspace, name, remote)
select distinct on (p.workspace, regexp_replace(rtrim(p.remote, '/'), '^.*[/:]|\.git$', '', 'g'))
       p.workspace,
       regexp_replace(rtrim(p.remote, '/'), '^.*[/:]|\.git$', '', 'g'),
       p.remote
from projects p
where p.remote <> ''
on conflict (workspace, name) do nothing;

alter table projects drop column if exists remote;
