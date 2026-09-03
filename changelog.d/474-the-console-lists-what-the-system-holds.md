**The console lists the flows, the roles, the skills and the hooks.** Four things a system holds had
one answer between them, and it was on the command line. An operator living in the console had to
leave it to find out what is running on its own, what a job can be run as, what a session is given
and what it runs under.

`:flows` is one line for each run of an automation graph: the graph and the version it pinned, where
it got to, and how many movements it has taken. A run waiting on a person shows the question where
the node would be, and a run somebody halted shows why. Backspace stops the run under the cursor and
asks first, the way it does on a job and on a session, and the reason it writes says a person did it.

`:roles`, `:skills` and `:hooks` are the catalogue. Each row says how far it reaches: the system
holds some of them, so every workspace has those without attaching anything, and the rest reach a
session only where somebody attached them. That is the question a name and a version cannot answer,
and it is why a workspace nobody attached anything to still holds four skills.

No row says why a skill is held and not given. That reason belongs to a workspace or to a session,
because what leaves a skill out is a secret one of them has not set, and these views ask for the
system's own catalogue. The control plane fills `left_out` on a workspace listing and on a session
listing and leaves it empty on this one, so a cell reading it would be empty on every row.
`krewe skill list <workspace>` still answers it. The console can answer it the day one of these views
is scoped to a workspace.

A run has no age column, and every other listing here has one. Nothing records when a run began: not
the wire, not the store, so the column could only draw a dash.

A run does not descend into its steps yet. Each step is a job under the job the run carries, and the
jobs view reads its scope as a project, so descending there would list every job in the project
rather than the four this run made. That needs a lister of its own.

This also answers issue 245, which asked for the skills view alone.
