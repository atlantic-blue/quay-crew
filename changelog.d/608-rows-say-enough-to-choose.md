**A workspace row and a project row now carry enough to choose by.** A workspace listed its name, its
identifier and its age, which says nothing about whether anything is happening under it. It now says
how many projects it holds, how much work is running, and whether anything under it is waiting for a
person. A project says the repository its work lands in, which is the fact that decides whether a job
declared there can finish at all, and the same two counts.

Both are read from one listing of the system's jobs rather than one call per row, so a screen of forty
workspaces costs three calls and not forty two. A system that cannot answer for its jobs draws the
listing with the counts absent rather than an error screen where the workspaces used to be.

**A job waiting for a person is marked in the row and counted above the columns.** The phase cell was
already yellow, and a colour is nothing at all on a terminal without one and nothing in particular in
a listing where half the rows are yellow already. There is a mark in the first column now, one
character wide, empty on every job that wants nothing, and it never gives way on a narrow window. The
line above the columns says how many are waiting, because a listing longer than the screen hides every
mark below the fold.
