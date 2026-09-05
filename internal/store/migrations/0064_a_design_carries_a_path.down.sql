-- Every path in the system goes with this table. The steps exist nowhere else: they are not on
-- `projects`, they are not on `project_designs`, and no file on the host holds a copy.
drop table if exists project_steps;
