-- Rollback for 0001_init. Never run automatically: migrations are forward only, and this exists so
-- an operator has an explicit, reviewed way back. It drops every session and project.

drop table if exists sessions;
drop table if exists channels;
drop table if exists projects;
