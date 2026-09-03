**Enter opens the task under the cursor.** Every row in the tasks view opened the same shell. The key
acted on the session the view is scoped to, and not on the row. The `asked` column holds 34
characters, so each row showed a fragment of a sentence. To read either task, a person left the
console and ran `krewe task list`.

Enter now puts the whole task on the screen: what was asked, and what came back or why it failed.
The console cuts neither of them. Any key closes the reading, and `j` and `k` scroll it. That is how
the console already shows what a command printed.

The conversation keeps `a`, which it already answered to. The shell keeps `s`. Both still act on the
session. A task has no container of its own, and a job whose session answered nothing lists no rows
to stand on. That reasoning was right for those two keys and wrong for enter. The thing on the row is
a task, and a reader wants the task.

The scenario is in [features/console.feature](features/console.feature).
