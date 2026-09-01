**A sandbox container is now named `krewe-<session>`.** The product is Quay Krewe. The command is
`krewe`. The containers an operator reads in `docker ps` said `quaycrew-`, which is a word nobody
types now. The name a sandbox starts under moves. The name it is found under does not move yet.

**Every reader takes both names.** They are the drain that puts a session down and the sweep
`make upgrade` runs. They are the listing that decides how much memory a session holds. They are the
reach into a session that ended, and the attach that opens a conversation in one. The system writes
`krewe-` and adopts either name. An operator who upgrades with sessions up keeps every one of them.

Without that half, each of those containers is invisible after the upgrade. Nothing drains it and
nothing removes it. The next task on that session starts a second container beside it. The first
container runs on, and it holds the machine's memory until a person finds it by hand.

The shape of the name is what makes two prefixes safe. A sandbox name is a prefix and exactly 24
hexadecimal characters. The compose stack's own `quaycrew-postgres-1` and its friends are not
sandboxes. A reader that took one for a sandbox would stop the system it runs in. That check is now
one answer in the sandbox package. It was a regular expression in one file and a prefix cut in
another.

**The local sandbox image is now `krewe-sandbox-claude:local`, tagged under both names.** An
operator's own configuration file pins `QC_SANDBOX_IMAGE`, and an upgrade never rewrites that file.
So `make sandbox-image` puts the retired tag on the same image, and `make env-check` names the key to
change. Without both, the first task after an upgrade fails on an image that is not there. That reads
as a broken system rather than as a rename.

**The compose project stays `quaycrew`, deliberately.** Compose puts the project name in front of
every volume it makes, so the database is `quaycrew_postgres-data`. A stack under a new project name
comes up on an empty volume. Every job, session, task and secret stays in the old volume. The stack
starts, the database is empty, and nothing says why.

A test holds the project to that name. The containers job in continuous integration starts the store
under it and reads back the volume the daemon made.

The read of the retired container name goes in the next release. The retired image tag goes with it.
Nothing else in the rename reaches a system that already runs.
