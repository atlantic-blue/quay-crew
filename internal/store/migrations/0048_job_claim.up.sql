-- A job says which piece of work it is doing, so a second job cannot take the same one.
--
-- The failure it answers: two sessions picked up the same issue, built it under different names, and
-- the first anybody knew was two pull requests conflicting on files both of them had created. Each
-- session already had its own working copy, so nothing was in the other's way in the filesystem. They
-- were in each other's way over the work itself, and no row anywhere said who was doing what.
--
-- An issue, a branch, or a name two people would both use for the same thing. It is stored lowercased
-- and with the space taken out of it, because a claim that misses over a capital letter is a claim
-- that did nothing.
--
-- No unique index on it, and that is the point rather than an omission. What is refused is a second
-- job claiming work another job still holds, and holding runs out: a job that settles releases its
-- claim, and so does one that nothing has moved for longer than a claim lives. An index cannot say
-- either, so the check is a read inside the writing transaction, under a lock taken on the claim.
--
-- Empty string rather than null, the way every other text column on this table already is: a reader
-- that has to tell null from empty is a reader with two cases where there is one.
alter table jobs add column if not exists claim text not null default '';

-- The read the check makes: one workspace, one claim, the jobs that have not ended.
create index if not exists jobs_claim_idx on jobs (workspace, claim) where claim <> '';
