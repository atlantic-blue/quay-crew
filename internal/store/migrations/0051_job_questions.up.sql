-- A job carries the questions a reading of its plan could not settle.
--
-- A plan used to be read by one role, in one session, once. One reading finds what that reading
-- looks for: a design named the address shape of a page, /videos?id=<video id>, and nobody asked
-- what a person types into it. A test writer asks that first, because an example needs an input and
-- an output. This system holds seventeen roles and the plan was read by one of them.
--
-- So several roles read the same plan, each writes what its own lens could not settle, and a later
-- reader settles what it can. Only a row every lens left open reaches a person, because a gate that
-- puts every question every reader raised is a gate a person stops reading.
--
-- Rows rather than an array column, the way the steps are: each one carries who asked it, who
-- settled it and when, and the primary key below is what makes a row carried twice leave one row.
-- The rows are handed down to the next reading and back up to the plan, so the same row exists on
-- more than one job under one number, and asked_in says which reading wrote it. The ceiling of
-- three counts by that: a reader may write three of its own however many it was handed.
create table if not exists job_questions (
    job        text        not null references jobs (id) on delete cascade,
    -- Where this question sits in the order they were asked, counting from one. It is the number a
    -- later reader settles by, so it is stable across the jobs a row is carried onto.
    seq        int         not null,
    text       text        not null,
    -- The role that wrote it, empty where no role ran.
    asked_by   text        not null default '',
    -- The job whose session wrote it, which is not the job column once a row has been handed on.
    asked_in   text        not null default '',
    status     text        not null default 'open',
    answer     text        not null default '',
    settled_by text        not null default '',
    asked_at   timestamptz not null default now(),
    settled_at timestamptz,
    primary key (job, seq)
);

-- One job's questions, in the order they were asked, which is the only way they are ever read.
create index if not exists job_questions_job_idx on job_questions (job, seq);
