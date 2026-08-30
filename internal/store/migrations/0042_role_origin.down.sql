alter table roles drop column if exists origin_repository;
alter table roles drop column if exists origin_commit;
alter table roles drop column if exists origin_path;
alter table roles drop column if exists origin_dirty;
alter table roles drop column if exists origin_unpushed;
