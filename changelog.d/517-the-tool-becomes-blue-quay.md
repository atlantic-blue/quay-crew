**The plan for the rename to blue quay is written down, and it is waiting for a person.** The owner
decided the name. `docs/RENAME.md` is the plan: seven decisions with a recommendation and a cost for
each, the four things a rename must not break on a build that is installed now, and the order of the
seven pull requests. Nothing is renamed yet.

Two hazards nobody had named turned up while measuring. The Postgres database lives in a volume named
after the compose project, so renaming that project starts an empty database and orphans every job,
session and secret. And the section mark in a session's memory file is `<!-- quay: -->`, which a
running session already wrote, so it cannot move. Both are answered in the plan, and both are why the
string `quay` does not leave the code in this pass.

One fact the plan repairs on the way. `github.com/atlantic-blue/krewe` is not a repository, so the
module path does not resolve and nobody can install this tool with `go install`. The new module path
matches the new repository address.
