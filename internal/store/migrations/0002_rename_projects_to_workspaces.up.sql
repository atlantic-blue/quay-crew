-- A project became a workspace, freeing the word "project" for the level being added beneath it.
--
-- This renames rather than recreates, so every existing workspace, channel and session survives with
-- its identifiers intact. In particular a session keeps its model_session_id, which is the only
-- pointer to a conversation the model holds on its own disk.
--
-- 0001 is left as it was written. Editing an applied migration would leave any database that already
-- ran it on the old names forever, since it is recorded as done.

alter table projects rename to workspaces;
alter index projects_live_idx rename to workspaces_live_idx;

alter table channels rename column project to workspace;
alter index channels_project_idx rename to channels_workspace_idx;

alter table sessions rename column project to workspace;
alter index sessions_project_idx rename to sessions_workspace_idx;
