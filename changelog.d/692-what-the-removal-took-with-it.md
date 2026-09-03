**`krewe answer` and `krewe read` survive the removal of jobs.** Both read what a session did, not
what a job did, and both were taken out with the job subsystem. `answer` is the way a reply leaves
the system as data rather than as a listing written for a person, and `read` hands back a file out of
a session's working directory. Neither has a replacement, so removing them removed a capability.

Every refusal for a removed word now names a command the tool still has. `krewe room`, `krewe
limits`, `krewe steer` and `krewe render` said what was gone and stopped there, which leaves an
operator with a word that no longer works and nowhere to go.

The migration drops with `cascade`. Naming each child table only works while the list of them is
complete, and one left off refuses the whole migration.
