**A parallel change does not collide in the changelog.** Six pull requests were open one evening and
four of them went `DIRTY` within seconds. Every conflict was `CHANGELOG.md` and nothing else: every
other file merged on its own, including the ones worth a human reading. The cause was the shape of
the file rather than any of the changes. Each one wrote its entry at the top of one shared file, so
any two changes written at once collided there by construction, and the resolution was always the
same and always mechanical. The crew paid a sandbox each to work it out again.

An entry now goes in its own file under [`changelog.d/`](changelog.d), named after the issue it
closes. Two changes write two different files and never touch each other, so there is nothing to
resolve. `make changelog` assembles every fragment into one dated section, newest first, in the shape
`CHANGELOG.md` already uses, and refuses a fragment it cannot trace back to an issue and a directory
with nothing in it. Assembling prints; whoever cuts the release pastes the section and deletes the
fragments, so a release stays one change a person read rather than a file a command rewrote.

`CHANGELOG.md` keeps the history exactly as it was. Nothing was migrated, because a record of what
shipped on a day is worth less once it has been rewritten, and because moving three thousand lines
would have collided with every branch in flight, which is the thing this change exists to stop.
