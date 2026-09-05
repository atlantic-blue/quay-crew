-- Every approval in the system goes with these columns. The word exists nowhere else: no file on
-- the host holds a copy, and a design that comes back after this reads as one nobody agreed to.
alter table project_designs
    drop column if exists approved,
    drop column if exists approved_at;
