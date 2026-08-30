-- What a role may call is called verbs, which is the word kubernetes uses for the same question.
--
-- The column was added as "may" eight migrations ago, and had to be quoted everywhere it was read
-- because the word is a reserved one in SQL. That quoting is the smallest of the reasons: the
-- manifest key is verbs now, and a column named after the word the crew retired is the next reader's
-- wrong turn.
--
-- A rename rather than a new column and a copy, so no row can carry two answers to one question.
alter table roles rename column "may" to verbs;
