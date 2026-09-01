**An address says which directory it is kept in, so a person can put a file in front of a session.**
Every level on disk is a generated identifier and none of the names is on the filesystem, so somebody
who knows they work in `atlantic-blue` had nothing to type. Getting a screenshot to a running session
meant reading three directories named in hex, then inspecting a container that happened to be up to
learn that a workspace's volume is bound at `/home/agent/shared`.

That last step had no answer at all once the containers were down, which is when the question is
usually asked.

`krewe where <address>` answers it. A workspace address answers with its shared folder, which every
session in that workspace reads. A session address answers with that session's own working directory.
The path is on the first line with nothing beside it, so `cd "$(krewe where atlantic-blue)"` works,
and under it is where a session sees the same directory, which is what to call the file once it is in
there.

It reads the layout rather than a container, so it answers with nothing running. It makes the
directory if it is not there: a workspace nobody has worked in has no folder yet, because the folder
is made when a sandbox starts, and a path that cannot be copied into is not an answer to somebody
holding a file.

The word `system` is refused rather than answered. The top of the data directory holds the system
token, the driver token and the key that unseals every secret, so a command that answers "where do I
put a file" must not offer a road to it.

`krewe workspace list`, `krewe project list` and `krewe sessions` each end with one line naming the
command, because a listing is where somebody looks when they are holding a file. A column would carry
a path of around a hundred characters, and `krewe sessions` already has thirteen columns.
