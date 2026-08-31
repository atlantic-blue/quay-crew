-- A job ends by stating one outcome from a fixed set, and that word is the signal.
--
-- Before this the signal was prose. Jobs on one acceptance run reported "done", "complete", "the pull
-- request is open" and "I could not finish because the credential expired". All four settled the same
-- way, because the crew read the sentence to decide the job was over. Two readings of one sentence
-- give two outcomes, so nothing downstream could branch and nothing could be counted.
--
-- The word is one of proved, unproved, blocked and decide, read off the answer by the controller and
-- never reported by the model. The answer stays where it is: it is the explanation, under the signal,
-- rather than the signal itself.
--
-- Empty string rather than null, the way every other text column on this table already is: a reader
-- that has to tell null from empty is a reader with two cases where there is one. Empty is every job
-- that has not settled, and every job written before this existed.
alter table jobs add column if not exists outcome text not null default '';

-- A listing filters by it, which is the read this exists for: what the system finished, and what it
-- could not do. Shaped like jobs_phase_idx beside it, because both narrow the same listing in the
-- same order.
create index if not exists jobs_outcome_idx on jobs (outcome, created_at);
