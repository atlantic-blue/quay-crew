-- Back to a session that runs until its context window is full.
--
-- Forward only in the control plane, which never applies a down file. This is here for an operator
-- who has to go back deliberately, and it drops every handoff a job recorded with it.
drop index if exists job_handoffs_job_idx;
drop table if exists job_handoffs;
alter table workspace_limits drop column if exists context_ceiling_percent;
