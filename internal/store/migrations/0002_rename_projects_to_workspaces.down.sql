-- Rollback for 0002. Never run automatically: migrations are forward only, and this exists so an
-- operator has an explicit, reviewed way back. It renames only, so no data is lost.

alter table sessions rename column workspace to project;
alter index sessions_workspace_idx rename to sessions_project_idx;

alter table channels rename column workspace to project;
alter index channels_workspace_idx rename to channels_project_idx;

alter index workspaces_live_idx rename to projects_live_idx;
alter table workspaces rename to projects;
