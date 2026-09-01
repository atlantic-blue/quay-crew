-- A job that names a repository does not settle on its own answer.
--
-- Every failure of the acceptance run reached the operator through one door: a session finished, it
-- wrote an answer, and the job settled on that answer. Where the work was wrong the answer said it
-- was right, in good faith, because the session had no way to know otherwise.
--
-- Two sessions that did not do the work now read it first. A reviewer reads the change against what
-- the job was asked for, and a tester runs the repository's own gates and reads their output rather
-- than their exit status. Both have to pass before the row moves to done.
--
-- Three columns, and they answer two different questions.
--
-- `ungated` is the caller's. The gate is refusable rather than optional, so a job may be declared
-- with it off, and the row says so. It is stated in the negative because a boundary has to default
-- on: a column defaulting to false is a job that is gated, which is what every row written before
-- this becomes.
--
-- `reviewed` and `tested` are the controller's, written in the same statement as the phase. A
-- settled job then always states whether anything independent passed it, without a reader having to
-- open two conversations to find out. They are written at the landing rather than as each gate
-- passes, because while the job is still running the answer is on the record of the gate's own
-- session, and a status a controller has to remember is a status the controller after it does not
-- have.
alter table jobs add column if not exists ungated  boolean not null default false;
alter table jobs add column if not exists reviewed boolean not null default false;
alter table jobs add column if not exists tested   boolean not null default false;
