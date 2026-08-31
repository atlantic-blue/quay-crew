**Work a job finished reaches somebody without a person carrying it.** A job that named a repository
and answered without a pull request used to stop on one sentence: the work is in the session and
nowhere else, open it and push what is there. The product of the job then sat where no command
reached it, and the operator became the transport.

The bytes were never in the container alone. A session's working directory is a bind mount the system
made itself, so the system was holding the work the whole time and had no way to name it.

So the system publishes rather than asking. Where such a job is about to stop, the system reads what
the session left behind and pushes the branch it is on. A push applies nothing, so it needs nobody's
approval; a merge runs the pipeline and a pull request is a decision, so the system does neither and
the reason says which step is left. Git runs inside the session's own container, which is where git
and the workspace's credential already are.

**The reason now says something an operator can act on, in every case.** Five outcomes are held apart
rather than collapsed: the branch is on the remote, the push was refused, the session committed
nothing, the session holds no repository, and the system could not look. The empty case is the one
that matters most, because a reason naming a branch nobody made sends somebody looking for work that
was never done. In every case but the first the reason names the directory the work is in, on the
machine running the sandboxes, and no reason sends a person into a container.

**`krewe read <session> [<path>]`** lists what a session made, or prints one file out of it. It reads
the directory rather than the container, so it answers for a session whose sandbox has gone, and it
pipes, which attaching never did.

**What this does not do.** It opens no pull request and merges nothing: both stay decisions. A local
sandbox is the host and has no container to run git in, so a local system says so and names the path
instead. A job that fails, rather than stopping without a pull request, is not published: its work is
still read with `krewe read`.
