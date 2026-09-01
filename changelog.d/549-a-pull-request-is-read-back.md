**The system reads back the pull request it opened, so "produced" stops being a guess.** A job that
names a repository ends in a pull request, and the address landed on the row. That was the whole of
what anything knew about the work. Nothing read the address again, so a change that merged and a
change whose checks went red an hour later read exactly the same.

A reader now asks the forge every two minutes about each pull request that has not merged or closed,
in one call each, and keeps four things on the job: whether it is open, merged or closed, what its
checks say and the name of the check that failed, whether a review asked for changes, and when it was
read. `krewe job show` prints them under the address. A merged or closed pull request is read once
more and then left alone, so nothing is paid for work that has stopped moving, and a job that opened
no pull request is never read at all.

A reading nobody took says unknown and never green. An operator picks up what is stuck on these
words, so a pull request that reads as fine because nothing could read it is the one they will not
look at. The reason sits beside it: a system with no forge credential says so, and names the command
that sets one.

The credential is the system's own secret, `GH_TOKEN` at the system level, because one process does
this reading. Set it once with `gh auth token | krewe secret set system GH_TOKEN`. Every workspace's
pull requests are read with it, and no page calls a forge while it draws.
