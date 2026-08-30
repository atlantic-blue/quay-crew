**A check reads the diff for a scenario and a changelog entry.** `CHANGELOG.md` opens with "anything
not listed here does not exist", and the line under it promises a reader a scenario in
[`features/`](features/) for each one. Nothing asked whether a change kept either promise. One change
shipped 200 lines of new behaviour, a rule that refuses a whole class of job brief, with neither, and
every check was green. Nothing was wrong with the checks: they were never asked the question, so the
promise held for exactly as long as whoever opened the pull request remembered it.

`make promises` now reads what a change touched. A change that touches behaviour, which is Go the
product runs or a contract it serves, has to carry a file under [`changelog.d/`](changelog.d/) and a
scenario under [`features/`](features/). A change may legitimately carry neither, so the way out is a
line in the pull request body rather than silence:

    No changelog entry: this renames a field nobody outside the package reads
    No scenario: the behaviour is unchanged, this moves it between packages

The reason is a sentence, so one word after the colon is refused. Whether the sentence is a good one
is the reviewer's to judge; the check only makes it impossible to say nothing at all.

A run that read no files refuses too. An empty diff keeps every promise there is, so a check pointed
at the wrong base ref would report success forever.

An author who writes their entry at the top of `CHANGELOG.md`, which is where every entry went until
[`changelog.d/`](changelog.d/) landed, is told where an entry goes now rather than told they wrote
none.
