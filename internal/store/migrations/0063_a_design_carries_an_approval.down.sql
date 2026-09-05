alter table project_designs
    drop column if exists approved,
    drop column if exists approved_at;
