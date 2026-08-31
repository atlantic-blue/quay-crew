-- A job records what each attempt at a step said, so the system can tell a session that is working
-- from one that is going in circles.
--
-- On the acceptance run of 30 August 2026 a session that could not get a check green tried the same
-- shape of fix several times and gave the same reasoning each time. Nothing compared an attempt with
-- the one before it, so the operator was the loop detector, and only where he happened to read the
-- transcript. From outside, a session going nowhere and a session working hard were one picture.
--
-- Rows rather than a counter, because the number that decides is a comparison and a comparison needs
-- both sides. They are also the corpus that replaces the threshold: every attempt writes its
-- similarity whether or not it loops, so the number can be measured on attempts rather than chosen
-- from prose. See the comment on job.LoopThreshold.
create table if not exists job_attempts (
    job         text not null references jobs (id) on delete cascade,
    -- The task this attempt was. It is the key rather than a counter because the same task is read
    -- again by whichever controller holds the job next, and an attempt counted twice would
    -- manufacture a loop out of one piece of work.
    task        text not null,
    -- Where this attempt sits in the order they were made, counting from one.
    seq         int  not null,
    -- Which step it was at, counting from one: the attempt after two finished steps is at step 3.
    -- Attempts are compared only with attempts at the same step, because a session that finished
    -- something is somewhere new.
    step        int  not null,
    -- The conversation it was made in. It stays here when a job that was handed to another role
    -- leaves that conversation behind.
    session     text not null default '',
    -- What the attempt had to show for itself: the answer where it answered, and the failure where it
    -- did not. Capped where it is written, and the similarity is measured on what is kept, so anybody
    -- holding this row can work the number out again.
    said        text not null,
    -- How like the closest earlier attempt at this step it was, between 0 and 1. Zero on the first
    -- attempt at a step, which has nothing to be like.
    similarity  double precision not null default 0,
    occurred_at timestamptz not null default now(),
    primary key (job, task)
);

-- One job's attempts, in the order they were made, which is the only way they are ever read.
create index if not exists job_attempts_job_idx on job_attempts (job, seq);

-- What this job does when it goes in circles, as the caller declared it: 'ask' to put the question to
-- the operator, or 'role:<name>' to hand it to another role. Empty is asking.
--
-- Declared rather than decided in the moment. The moment a job is going nowhere is the worst moment
-- to be working out what to do about it, and the answer depends on the work rather than on the loop.
alter table jobs add column if not exists escalation text not null default '';

-- Which step it went in circles on, and what it escalated to, in the shape the route was declared.
-- Zero and empty for a job that never has. A job escalates once: escalated_to being set is what makes
-- a second loop stop the job rather than escalate it again, which would be the system going round the
-- same loop with more steps in it.
alter table jobs add column if not exists looped_step integer not null default 0;
alter table jobs add column if not exists escalated_to text not null default '';
