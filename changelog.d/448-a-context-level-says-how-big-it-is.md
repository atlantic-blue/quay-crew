**A level of context says how big it is, and warns when it is large.** The crew level held 100,179
characters. Every session in every workspace carries that level before it reads a line of the
repository it works on, so a rule added there is a rule nobody finds. On 29 August 2026 a cost rule
went onto a workspace instead, because the crew level would have buried it, and that undoes the whole
point of having a crew level.

Nothing anywhere reported the size. Reading it meant `select scope, owner, length(body) from contexts`
against Postgres.

Now `quay context` carries a characters column, and every write says what it wrote. Past 20,000
characters the level also says who reads it and what to move down a level. The console's context view
shows the same number, drawn in yellow once it is over the mark, because the console and the command
line are two clients of one call.

The mark is a mark and not a limit. Nothing is refused for being over it: a crew that wants a long
level keeps one, and the point is that it chose to.
