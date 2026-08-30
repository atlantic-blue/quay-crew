# changelog.d

One file per change. This is where a changelog entry waits for a release.

Entries used to be written straight into the top of [`CHANGELOG.md`](../CHANGELOG.md). Two changes
made at the same time wrote the same lines of the same file, so they collided every time, and the
resolution was always the same: keep both. Six pull requests were open one evening and four of them
conflicted. Every conflict was that one file and nothing else.

A fragment is a different file per change, so two changes never touch each other.

## Writing one

Name it after the issue it closes, then words joined with hyphens:

    changelog.d/480-changelog-fragments.md

Write the entry as ordinary markdown, starting with the sentence that says what changed:

    **A parallel change does not collide in the changelog.** Every change used to write its entry at
    the top of one shared file, so any two changes written at once collided there by construction.

Say what changed and why it changed. The changelog is the honest record, so an entry that claims more
than the code does is worse than no entry.

A link is relative to the root of the repository, because that is where the entry ends up. Write
`[features/](features/)` rather than `../features/`.

## Releasing

    make changelog

That prints every fragment as one dated section, newest first, in the shape `CHANGELOG.md` already
uses. Paste the section under the heading in `CHANGELOG.md` and delete the fragments in the same
commit. The command writes nothing itself, so a release stays one change a person read.

It refuses a name it cannot trace back to an issue, an empty fragment, and a directory with nothing
in it. An assembled release that says nothing looks exactly like one that assembled correctly.

`CHANGELOG.md` keeps everything that landed before this convention. Nothing was moved into here: a
record of what shipped on a day is worth less once it has been rewritten.
