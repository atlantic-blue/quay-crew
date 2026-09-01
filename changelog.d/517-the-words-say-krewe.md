**The documents say krewe.** Every command in the README, in `docs/`, in the scenarios, in the flows
and in the roles is now the command a person types. They said `quay task`, `quay job` and
`quay secret`. None of those ran after the command moved. A reader who copied a line out of
the documentation got "command not found".

**The product is Quay Krewe.** The name in the README, in the architecture document and in the manual
a session reads was Quay System. The console's status block said `Quay:` when a control plane is
older than the tool, and it says `Krewe:`.

**Three names in the documents were already stale, and this corrects them.** The sandbox image is
`krewe-sandbox-claude:local`. A sandbox container is `krewe-<session id>`. The tmux session an open
conversation lives in is `krewe`, and the panel's window is `krewe-panel`. Each was renamed in an
earlier piece or was never what the document said.

**What keeps the old word, and why.** The changelog is a record: in August the tool was called quay,
and an entry that says otherwise makes the past false. `docs/RENAME.md` names the directory that
went, for the same reason. Two captured runs in `docs/ORCHESTRATION.md` show `which quay` answering
from inside a container, and a captured run is evidence rather than prose. The compose project, the
Postgres database and user, the protobuf package and the metric names are all still `quaycrew`.
Section 4 of `docs/RENAME.md` says why for each one.

The repository address is still `quay-crew`, so every link in these documents still resolves. It
moves with the module path, in the piece after this one.
