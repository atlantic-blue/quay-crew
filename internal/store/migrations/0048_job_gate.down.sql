-- Back to a job that settles on its own answer, with nothing independent having to agree.
--
-- Forward only in the control plane, which never applies a down file. This is here for an operator
-- who has to go back deliberately, and it drops the record of what read each job before it settled.
alter table jobs drop column if exists tested;
alter table jobs drop column if exists reviewed;
alter table jobs drop column if exists ungated;
