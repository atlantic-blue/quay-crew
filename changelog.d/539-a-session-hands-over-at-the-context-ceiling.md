**A session stops taking work before its context window is full, and hands the rest of its job to a
fresh one.** A session used to run until the window was full. The system printed the share in a column
and did nothing with it, so a session at eighty per cent kept taking tasks, and the last task of a long
job is the one that opens the pull request and writes the answer. The work that mattered most was done
where the model is worst, and nothing failed: the job finished and read like one that went well.

Past the workspace's ceiling the system gives that session no new task on the job it is doing. It asks
for one thing instead. The session writes down what is left and what it tried that did not work with
`krewe job handoff "<what is left>" "<what you tried>"`, and the rest of the job goes to a conversation
with an empty window, which is given the brief, the steps already finished, and those words. The job
does not restart: same identifier, same steps, same pull request, one more attempt.

`krewe limits <workspace> --context-ceiling <per cent>` sets it. It ships at 70, and `krewe limits`
prints where that came from beside it: the standard named in the issue, which says quality falls off
between 50 and 70 per cent of a window and is poor past 70. **No measurement of this system produced
it.** What would replace it is a measurement of this system's own answers against how full the window
was when each was written, and nobody has taken one. A workspace that wants no gate sets 100.

The session listing says what the share means rather than only what it is: `82% over` for a session
that takes no new work, `55% near` for one inside the twenty points below the ceiling, which is the
band the same standard names.

**Two silences would each undo this, and both are refused.** A window nothing has measured refuses
nothing, because the size of a window is what the model runtime last told a session and a system nobody
has told would otherwise stop every job on it. And a session that writes no handoff stops its job
rather than having a fresh one started from nothing: that session would pay for every discovery the
last one made, and the job would then read exactly like one that handed over well. The store refuses
the movement in the same statement, so a job never leaves its session with nothing to carry.

**What this costs, and what it does not do.** A fresh session starts in an empty working directory,
because nothing clones a repository once per workspace yet ([#255](https://github.com/atlantic-blue/quay-crew/issues/255)).
So the ask tells the session to push its branch and name it in what is left, and the handoff carries
prose rather than a checkout. Compacting and continuing, the other road the issue names, is not built:
nothing in the system can make a model runtime compact. Nothing here says what a session spent its
context on, which is [#541](https://github.com/atlantic-blue/quay-crew/issues/541) and is the
measurement that would replace the number above.
See [#539](https://github.com/atlantic-blue/quay-crew/issues/539).
