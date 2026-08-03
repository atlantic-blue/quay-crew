-- Rollback for 0003. Never run automatically. Sessions keep their workspace, so no conversation
-- handle is lost, but which project a session belonged to is gone.

drop index if exists sessions_project_idx;
drop index if exists sessions_project_thread_idx;
alter table sessions drop column if exists project;
alter table sessions add constraint sessions_workspace_thread_id_key unique (workspace, thread_id);

drop index if exists projects_workspace_idx;
drop table if exists projects;
