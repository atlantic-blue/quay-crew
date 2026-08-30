**A role says where it came from, so a role nobody reviewed is visible as one.** A role is imported
from a directory, and a directory is anywhere. That made the first import easy and everything after
it invisible: the acceptance run was driven by three roles that sat in a folder on one machine, so no
pull request touched them, nobody reviewed them and nothing versioned them, while every listing the
crew printed showed them looking exactly like the fifteen that ship in [`roles/`](roles). The clause
that decided the whole run was read by the session it was handed to and by nobody else.

`quay role import` now records where it read the files: the repository, the commit, the directory
inside it, whether the files were edited after that commit, and whether the commit is on a remote
branch. `quay role list` and `quay role show` say it back, and a role nobody else could open says so
in a line of its own with what to do about it.

Nothing is refused over it. A role written in a scratch directory while somebody finds the shape of
it is ordinary, and what was missing was not a gate, it was anybody being able to see. Importing the
same role again from a repository records where it was read this time, so committing a loose role and
importing it again clears the warning rather than leaving the operator to wonder why nothing changed.

Where it came from is not part of what a role is, so it is not in the fingerprint: the same bytes
read out of two checkouts are one role, read in two places, and a version already imported is not
refused as a different role because somebody else imported it.
