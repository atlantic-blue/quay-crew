-- The operator's word on a design body, and when it was given.
--
-- The approval is a statement about one text. Any write to `body` clears it, which the write itself
-- does rather than a trigger, so the rule sits beside the statement a reader is looking at.
--
-- `approved_at` is null while `approved` is false. Two columns rather than a nullable stamp alone,
-- because every read asks the question as a boolean and a null timestamp read as a false is the kind
-- of thing that only shows up in the one place it was forgotten.
alter table project_designs
    add column if not exists approved    boolean not null default false,
    add column if not exists approved_at timestamptz;
