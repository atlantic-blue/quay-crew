**The console's jobs view shows the work a person declared, and keeps the parts under it.** A job in
its test stage fans out into one part for each requirement. Until now every part was a row beside the
job that declared them, so a fan out of five drew six rows with nothing saying which was which, and
the job somebody asked for was the one at the bottom of the six.

A part is now drawn under its job, and only when somebody asks for it. Tab opens the parts of the job
under the cursor and tab again takes them away. The job's row says how many are under it, so the key
is worth pressing before anything is pressed: `▸5 read the electricity bill`, and `▾5` while they are
open. A part is indented under the job it belongs to, and enter on it opens that part's own work.

The other way onto a part is the filter, which is why a filtered listing stays flat: typing `/` and
part of a title finds a part without opening the job above it first.

The cost is three characters of the title column on a job that fanned out, and two on each part. At
one hundred columns the title cell holds about twenty two characters, so a long title on a fanned out
job is cut three characters earlier than it was.

Nothing new is recorded. Each row already carried its parent, and the console counts the parts off
the listing it already had.
