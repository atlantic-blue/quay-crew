**The command is called krewe.** Quay is Red Hat's container registry. The same audience, the same
words, and this tool runs containers, so the name had to go. A krewe is the group that puts the work
on, and the name is free everywhere: no package, no formula, no repository.

Type `krewe` where you typed `quay`. Nothing else changes: the same commands, the same arguments, the
same system. `make install` builds both names and puts them where the old binary already was.

The old name is still on your path, and it refuses. Every invocation exits non zero and says what to
type instead, whatever follows it, because the word left off a list of remembered commands is always
the word somebody types next. A rename that leaves nothing behind answers with "command not found",
which reads as a broken install rather than as a rename.

The Go module path moved with it, and the piece that renames the repository moves it again, to
`github.com/atlantic-blue/quay-krewe`.

This is the first piece of [#517](https://github.com/atlantic-blue/quay-crew/issues/517). The rest of
the rename is planned in [docs/RENAME.md](docs/RENAME.md), and each piece says what it moved in its
own entry here.

Some names do not move at all, and the plan says why for each one. `QC_TOKEN`, `QC_GRPC_ADDR`,
`QC_SESSION_ID` and `QC_TRACEPARENT` are read by sessions that are already running. Neither does the
mark a memory file carries, `<!-- quay:session -->`, for the same reason: a session that is up wrote
its file under that mark, and a build that stopped reading it would sweep every level of that file
into one.
