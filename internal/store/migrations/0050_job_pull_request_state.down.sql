-- Reverses 0050. The readings go with the columns: they are what one build learned from a forge, and
-- a build that cannot read one has nothing to do with them.
drop index if exists jobs_unsettled_pull_request_idx;
alter table jobs drop column if exists pull_request_failed;
alter table jobs drop column if exists pull_request_read_at;
alter table jobs drop column if exists pull_request_review;
alter table jobs drop column if exists pull_request_check;
alter table jobs drop column if exists pull_request_checks;
alter table jobs drop column if exists pull_request_status;
