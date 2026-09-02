alter table workspace_limits drop column if exists waiting_seconds;
alter table jobs drop column if exists raised_at;
alter table jobs drop column if exists asked_at;
