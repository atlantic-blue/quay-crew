-- Back to a plan read by one role, whose questions live nowhere.
--
-- Forward only in the control plane, which never applies a down file. This is here for an operator
-- who has to go back deliberately, and it drops every question every reading wrote.
drop index if exists job_questions_job_idx;
drop table if exists job_questions;
