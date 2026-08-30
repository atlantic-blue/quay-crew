**A level's context can be read back.** `quay context` printed which levels were set and the first
few characters of each, and nothing printed what a level actually said. So a level could only be
overwritten: adding a paragraph meant already holding the whole text, and during an acceptance run
the only way to recover a workspace's context before adding to it was to read the `contexts` table in
Postgres directly.

`quay context show [<address>|crew]` prints the body to standard output, byte for byte, and nothing
else. It is the other half of `quay context set`, so this reads a level out and writes it back
unchanged:

    quay context show crew > file
    quay context set crew < file

That also means a level can be diffed, piped, and kept in a repository and compared against what the
crew holds.

A level that says nothing exits non zero and names how to write it, rather than printing silence: an
empty file and a clean status is what a broken read looks like too.

The listing behind it stopped dropping rows. A project's context is held in the store, and the row
describing it was skipped whenever the crew could not name the directories on disk, so a control
plane started without a data directory reported every project as saying nothing. `quay context clear`
read the same rows, so it announced a level as already empty while it held a body.
