**A continued job says what moved under the base it stands on, and the system reads the answer.** A
resume puts a session back into the working directory it left, and what that work stands on moved
while it was stopped. The session was already asked to fetch and say what moved, and nothing read the
answer, so an attempt that never looked at its base ended as done and read like an attempt that went
well. That is the second failure a resume can cause, and it is the one the operator cannot see.

So the continued task now asks for one line opening with `Base:`, for example `Base: nothing moved` or
`Base: origin/main moved on by four commits`. Where the job names a repository and the answer carries
no such line, the session is asked once more, and a second answer that carries none stops the job
saying so. Asked once and no more: every ask is a task somebody pays for.

**What this costs, and what it does not do.** The system still runs no git, so what it holds is what
the session said rather than what the repository did. A stopped job is terminal, so a continued
attempt that will not say what moved ends as an attempt somebody has to read: what it produced stays
on the row, the pull request address included, and the reason says where to look. A job that names no
repository is not held to this at all, because the system knows of no base it was away from.
