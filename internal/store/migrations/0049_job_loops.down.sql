-- Reverses 0048. The attempts go with it: they are the record of a comparison this build makes, and
-- a build that does not make it has nothing to read them with.
drop index if exists job_attempts_job_idx;
drop table if exists job_attempts;
alter table jobs drop column if exists escalated_to;
alter table jobs drop column if exists looped_step;
alter table jobs drop column if exists escalation;
