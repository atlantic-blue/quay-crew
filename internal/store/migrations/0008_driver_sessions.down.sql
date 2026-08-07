drop index if exists sessions_one_driver_per_project;
alter table sessions drop column if exists driver;
