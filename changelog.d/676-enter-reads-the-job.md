**Enter on a job reads that job.** Enter opened the conversation of the session under the job, which
is one level past the row a person pointed at. The job itself had no screen at all: what it is, the
sentence it serves and which of the four stages it stands in were only at the command line, so a
person watching a build left the console and ran `krewe job show`.

Enter now opens the job on its own screen. The top of it holds what does not move while you read:
the title, the stage as "stage 2 of 4: design" beside the phase, and one line for each session
working on it. A job that fanned out runs one session for each vertical and holds none of its own, so
those lines name the runs. Under that is the sentence the job serves, what the stage before it closed
on, what opens the next one, and what a person said about what it understood. That part scrolls with
`j` and `k`, and the session lines stay where they are: a laptop pane is about twenty four rows, and
the lines that say which sessions are working are the first thing a body of prose pushes off the
bottom.

What the job did keeps a key of its own. `t` opens the tasks of its session, which is where enter
used to go, and it is the same key the sessions view already opens a history under. A part of a job
is a run of a stage rather than a job, so enter on one still opens what that run did.

The screen is built where the listing is read, so opening a job costs no call to the system.

The scenarios are in [internal/console/onejob_test.go](internal/console/onejob_test.go).
