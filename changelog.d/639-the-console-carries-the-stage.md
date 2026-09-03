**The console's jobs view carries the stage.** The phase says what the system is doing with the row:
it is pending, it is running, it is asking. It never said how far through the work the job is. A job
waiting for an answer about what it understood and a job waiting for an answer about a failed build
both read `asking`, and those two are days apart.

The stage sits beside the phase, read by the package that decides it, so the console, `krewe job
list` and `krewe job show` cannot say three different things about one row. A job that runs no stages
shows `-` rather than naming one it is not in.

The table had no room, which is why this waited. Four columns were wider than anything they hold: the
job and the session columns are eight characters of identifier, the phase is seven, the outcome is
eight, and the count of attempts was under a heading five characters wider than the number. Trimming
them paid for the stage, and the title, which is the line an operator reads to know what the job is,
is as wide as it was before. The count of attempts is headed `tries`.
