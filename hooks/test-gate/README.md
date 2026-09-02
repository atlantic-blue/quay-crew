# test-gate

Refuses a session that builds against failing tests when it changes one of them.

The build stage hands a worker a suite that is already red and asks it to make the tests pass. The
shortest way to a green suite is to change the assertion. A session that does it is not dishonest.
From the inside, a failing test looks exactly like a wrong test, and nothing tells the two apart.

The rule was advice for as long as it existed. Advice is what a model weighs against everything else
it was told, and a boundary a session can talk itself past is not a boundary. So this is checked at
the moment the session tries, by a process the session does not control.

## When it is on

Only for a session the system set `KREWE_BUILDING` on, which is a worker in the build stage and
nothing else. Every other session is refused nothing here. That matters. The stage before this one
writes the tests, and a gate that fired for every session would refuse the worker that fills the
suite.

A command that sets or clears the variable is refused, whether the gate is on or off. A session that
decides its own boundary has none.

## What it refuses

A write to a test, in every shape the session can reach for.

- **The write tools**: `Write`, `Edit`, `MultiEdit` and `NotebookEdit`, when the path is a test.
- **The shell**: a redirect into a test. `sed -i` and `perl -i` on one. `rm`, `mv`, `cp`, `tee`,
  `touch` and `patch`. `git checkout` or `git restore` of a test path. Under `sudo`, inside a loop, after `&&`, and inside
  a shell of its own. The reader takes the line apart the way a shell does.

## What it allows, on purpose

**Reading.** `cat`, `grep`, `sed -n '1,40p'` and running the suite are all allowed. This is the
deliberate difference from the discipline the stage comes from, where the implementer never sees the
test at all. A build that cannot read the test cannot tell a failing assertion from a broken one, so
it guesses instead.

## What makes a file a test here

The name, because the runner decides the same way. A name that ends in `_test.go`, `.spec.ts`,
`_spec.rb` or `.feature`. A name that begins with `test_`. A path under `test`, `tests`, `spec`,
`__tests__`, `features`, `fixtures` or `testdata`. A fixture is what a test asserts against. A
change to one changes the assertion, and it touches no file named as a test.

The cost of a rule about names is that a repository whose deliverable is itself a test harness has
files this refuses. The way through is the same as for a wrong test. Say so in the answer.

## The way through

The refusal names the file. It says why the file reads as a test. It says what to do instead. Read it. If the test
itself is wrong, say so in the answer, name the file and the assertion, and say what it must assert.
A person decides that.

The stage reads an answer of that shape and puts it to somebody. So a worker between a broken test
and this boundary is never stuck for long.
