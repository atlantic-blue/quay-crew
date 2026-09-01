**The console opens on the workspaces now, and each key goes one level down.** It used to open on a
flat list of every session in the system. One operator command makes eleven sessions, and eleven rows
with nothing above them say nothing about what any of them is for.

There are four levels and you enter at the top. A workspace opens its projects. A project opens the
jobs declared in it. A job opens the running work: the tasks its session ran, and what each one
produced. Enter goes one level down. Escape comes one level back, from every level, including the
deepest one.

Enter on a project used to open its sessions. It opens the jobs now, so the sessions of one project
moved to `s` rather than to a trip through the command bar. Nothing else moved. Every flat listing is
still one word in the command bar, so `:sessions` still lists every session the system holds.

The deepest level is where a person watches something happen, so the two keys that reach the machine
live there: `enter` opens the conversation and `s` opens a shell in the sandbox. Both act on the
session the level is scoped to rather than on a row, because a job whose session has answered nothing
yet lists no rows, and that is the job somebody is most likely to be watching.
