-- What a thread is called, in two columns rather than one.
--
-- label is what the operator typed. description is what the crew observed the conversation to be,
-- written by the model that had the turn. They are separate because they answer to different people:
-- a label is never overwritten by anything automatic, and a description is rewritten whenever the
-- conversation has moved on. A listing prefers the label when there is one, because a name somebody
-- picked beats a name a machine wrote.
--
-- Both default to empty rather than being nullable. An unnamed thread and a thread named "" are the
-- same thing, and two spellings of the same thing is how a listing ends up with a blank cell nobody
-- can explain.
alter table sessions add column if not exists label text not null default '';
alter table sessions add column if not exists description text not null default '';

-- The turn count when the description was last written, so re-describing has something to compare
-- against. Zero means never described.
alter table sessions add column if not exists described_at_turn integer not null default 0;
