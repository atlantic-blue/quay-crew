**A session says where its context went, and the first measurement says no category dominates.** The
system printed how full a window was and stopped there, so a session at eighty per cent could have
filled up on the code it had to read, on tool output it read once, or on its own repeated attempts,
and nobody could say which. A share nobody can act on is a display.

Every conversation is now read back and split four ways. `reads` is the contents of files, however the
session opened them, with a reading tool or with a reading command in the shell. `tools` is what every
other tool returned. `turns` is the session's own words, thinking and calls. `told` is the task it was
given and the answers to its questions. `krewe sessions` carries a `spent on` column naming the
largest, the console carries the same column, and `krewe job show` prints the whole breakdown for the
session the job ran in.

The four add up to the total, and the total is printed against the model's own count of the same
context, because a breakdown whose parts do not add up to the model's total is a number that will be
trusted and is wrong. What no transcript holds, the system prompt and the definitions of every tool
the session carries, is named rather than folded into the four.

It costs nothing to say: the cost, the window and the breakdown all come out of one pass over the
transcript, kept until the file changes.

`docs/CONTEXT-SPEND.md` holds the measurement, and the harness that produced it ships beside it so
anybody can repeat the run. Over 192 conversations and 42,839,991 characters, reads take 31 per cent,
tool output 33 and the sessions' own turns 32. **No category dominates**, so a change to how sessions
read code is worth about a third of the budget at best. Any proposal to change how sessions read cites
that measurement.

The run also replaced a rule of thumb with a number. Four characters a token is prose; a session's
conversation is code, paths and terminal output, and it measures 1.883. And counting only what a
reading tool returned would have put reads at 2 per cent, because the sessions here are told to work
through the shell and read their files with `cat` and `sed -n`.

See [#541](https://github.com/atlantic-blue/quay-crew/issues/541).
