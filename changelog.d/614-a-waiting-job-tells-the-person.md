**A job that stops for a person tells them.** On 1 September 2026 four jobs put a plan up for
approval and stopped. Nothing told anybody. The oldest waited more than one hour, and the person they
were waiting for found out because he asked what the state was.

Everything that could answer "what waits on me" waited to be opened. The briefing is a page, the job
listing is a command, and the console drew the phase to whoever was looking at it. The transition
wrote `job.asked` to the event log and nothing read it.

The crew now answers it in one read, `GetWaiting`, and the surfaces a person already has open say it.
The console rings the terminal bell once when the count goes up, which reaches a tab that is not in
front, and draws one line above whatever view is open. Any `krewe` command prints the same telling
above its own output, on the error stream, so a listing piped into something else is still only the
listing. The line under an attached conversation carries the count beside the context window, asking
the crew at most once every three seconds however often the runtime redraws.

Three kinds of wait, because a person is needed for all three: a job that is asking, a job that
failed or was stopped or has no room, and a job whose pull request the forge says is red. What each
one wants goes through the same redactor a record goes through, so a sealed value a question quoted
never reaches a screen.

Past a limit the telling names the age: "waited 1 hour 4 minutes". The limit is a workspace value on
`krewe limits --waiting`, and it ships at fifteen minutes. That number is a guess and the code says
so. What replaces it is the median time from `job.asked` to `job.raised` over one week of real jobs.

`job.raised` is the new record, written by the first surface to name a waiting job and carrying which
surface that was, once for each wait rather than once for each redraw. `krewe job show` prints both
moments and the gap between them. That gap is measured from where the wait in hand began: see
`changelog.d/637-the-wait-gap-belongs-to-the-wait.md`, which corrects what this claimed.

Nothing leaves this machine. No phone, no chat, no mail: reaching a device off it needs a credential
the crew does not have, and that decision is recorded in `docs/ARCHITECTURE.md` under 31 August 2026.
So a person with no surface open is still told nothing, and the chat channels are what close that.

The ideation gate stamps the wait the same way. A question about what a job understood is one of the
three kinds of wait, so the two moments go on the row there as they do at the ask and at the plan.
Both stores are held to it by the shared conformance suite.
