**Declaring a job says which skills the session will start without.** A workspace was created, a
project was created, three roles were attached, and nothing anywhere said the workspace held no
credential. Every session in it would have died on its first clone, after the budget had been spent
on starting them.

The crew already knew. `quay skill list` printed the reason unprompted, naming the skill, the secret
and the command that sets it. That was one listing nobody is required to read, so `quay job create`
now prints the same sentence, at the moment somebody declares the job.

It says rather than refuses. The crew cannot know which skill a brief will reach for, so refusing
would stop a job that reads an electricity bill over a forge token it never wanted, and one unset
secret would be enough to stop every job in the workspace. That is the trade a session already makes:
a skill whose secret is missing is left out of the sandbox and the task still runs.

A job whose role does not receive skills says nothing, because a role that was never given them has
no gap to report, and a note printed every time is a note nobody reads.
