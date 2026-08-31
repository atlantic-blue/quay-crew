drop index if exists jobs_claim_idx;
alter table jobs drop column if exists claim;
