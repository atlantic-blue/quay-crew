-- The score of a job is how many times the operator had to steer it, and until now that number was
-- counted by hand.
--
-- A steer is one moment the operator had to say something the system should have known, asked for,
-- or refused on its own. Thirteen of them across two days of the acceptance job were written out
-- afterwards into a markdown file, numbered by the person who watched it. Nothing in the system knew
-- any of them happened, so no improvement could be measured against the one before it.
--
-- The row keeps the job it landed on and the job at the top of that tree. The second is what makes a
-- whole tree's marks one query, and it cannot go stale: a job's parent is set when it is declared
-- and never moves.
create table if not exists job_steers (
    id          text primary key,
    job         text not null references jobs(id) on delete cascade,
    root        text not null references jobs(id) on delete cascade,
    workspace   text not null,
    project     text not null,
    text        text not null,
    occurred_at timestamptz not null default now()
);

-- The report reads one tree in the order the marks were made, so that is the index.
create index if not exists job_steers_root_idx on job_steers (root, occurred_at);

-- The count is on the job as well as in the rows, written in the same transaction, so a listing and
-- `krewe job show` say the score without reading a second table. A steer counts on the job it landed
-- on and on every job above it, which is what makes the number on the job at the top the score of
-- the whole tree.
alter table jobs add column if not exists steers integer not null default 0;
