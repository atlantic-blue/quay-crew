-- The operator's word on a design, and when it was given.
--
-- Approval is a statement about one text, so any write to `body` sets `approved` back to false in
-- the same write. A row that said approved while carrying a body nobody read would be worse than a
-- row that said nothing.
--
-- Nothing inside a sandbox writes these columns. The call that sets them is refused to the driver's
-- token, so the word reaches the store only through the operator's own command.
alter table project_designs
    add column if not exists approved    boolean     not null default false,
    add column if not exists approved_at timestamptz;
