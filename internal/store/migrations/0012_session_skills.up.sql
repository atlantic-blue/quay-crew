-- What skill set a thread's live sandbox was born with, as one fingerprint. Empty means no live
-- sandbox is known, so the thread can never be stale: its next sandbox is born with the current
-- set. Compared against the workspace's current skills to mark a running thread stale.
alter table sessions add column skills_fingerprint text not null default '';
