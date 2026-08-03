-- Reversing this loses which threads were armed, and nothing else. Every turn goes back to the one
-- hardcoded mode.
alter table sessions drop column if exists permission_mode;
