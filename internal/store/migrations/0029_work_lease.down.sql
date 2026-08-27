drop index if exists work_lease_idx;
alter table work drop column if exists lease_until;
alter table work drop column if exists lease_owner;
