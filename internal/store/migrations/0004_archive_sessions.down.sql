-- Reversing this loses which threads were put away, and nothing else: the rows, the conversations
-- and the project files were never touched.
drop index if exists sessions_live_idx;
alter table sessions drop column if exists archived_at;
