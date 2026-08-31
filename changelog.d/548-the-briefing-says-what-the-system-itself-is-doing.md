**The briefing carries a line about the system, and it draws itself again.** The front door answers
what waits on you, what is blocked and what the system produced. Above those blocks it now says how
many jobs are running, what the system has spent in tokens, what the machine has left and what the
last health probe found, in the words `GetHeadroom` and `GetHealth` already use, so this line, the
console and `krewe header` cannot disagree about one system. A figure nothing measured reads unknown,
and a system that has never probed itself reads not checked: a reading nobody took must never look
like a healthy one.

The page also redraws itself every fifteen seconds and says the moment it was drawn. A page that sits
in a tab and looks current is the failure the briefing exists to end, and a reader who has to remember
to reload is a reader reading yesterday. Following one job as it moves, rather than redrawing the lot,
is https://github.com/atlantic-blue/quay-crew/issues/334.

**A job waiting on you gets the command that actually answers it.** Every waiting row used to offer
`krewe job answer`. A job carrying a flow run is answered through the run, and `AnswerFlowRun` refuses
anything that is not a run, so that row was handing the operator a refusal. It now offers
`krewe flow answer` with the identifier the call accepts.

**A listing can be read by the moment a job finished.** `ListJobs` takes `finished_since` and `limit`.
Naming a moment turns the order into most recently finished first, because a caller asking what
finished lately is not asking when a job was declared, and the two are not in step: a job declared this
morning can finish before one declared last week. A job that has not finished is left out. A cap below
zero is refused with a sentence saying what to send instead, and a request that sets neither behaves as
it did. Both stores answer the same, held there by the conformance suite, and a migration adds the
index on `finished_at` that the read needs, which the table did not have.

**What this does not do.** It says nothing about whether a pull request merged or whether its checks
went red, which stays https://github.com/atlantic-blue/quay-crew/issues/549, and it reports no money,
because the store holds tokens and not dollars.
