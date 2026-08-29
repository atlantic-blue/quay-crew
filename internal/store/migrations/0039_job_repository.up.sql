-- A job says which repository it works in, and where its pull request went.
--
-- Both are on the job rather than in a brief. A brief that forgets to ask for a push produces work
-- nobody can see, and every brief forgets eventually: the acceptance run took three hours and left
-- one readable thing at the end, because nothing in the tool said a phase ends in a pull request.
--
-- repository is what the caller declared, written owner/name. pull_request is what the crew read off
-- the answer, so a listing of jobs says where the work went without anybody opening a sandbox.
--
-- Both default to the empty string rather than null: every other text column on this table already
-- does, and a reader that has to tell null from empty is a reader with two cases where there is one.
alter table jobs add column if not exists repository text not null default '';
alter table jobs add column if not exists pull_request text not null default '';
