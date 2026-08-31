**The web view answers the operator's questions instead of listing sessions a third time.** `krewe
sessions`, the console and `krewe web` all drew the same four columns, and the question they answered
was the one an operator asks least: what is running. Four jobs ran at once and everything he knew
about them reached him because another person typed it into a terminal for him.

The front door of `krewe web` is now the briefing. It answers three questions in the order a decision
needs them: what is waiting on you, what is blocked, what the system produced. What is running comes
last. The session listing keeps its page at `/sessions` and loses the door.

A row carries what the system holds about the job: the question it asked and the command that answers
it, the reason it stopped, the pull request it opened, and what it spent in tokens. A block with
nothing in it says so in a sentence, because a page with nothing blocked and a page that failed to
read the system must never look the same.

Jobs are drawn as the tree in [docs/ORCHESTRATION.md](docs/ORCHESTRATION.md) section 12 rather than as
a flat list: a child that asked a question is drawn under the work it belongs to, with the session as
a cell on the row.

**What it says about a pull request, and what it will not say.** The system keeps the address and has
never read it back, so a row says the checks were not read. It never says they passed. Reading a forge
is [#549](https://github.com/atlantic-blue/quay-crew/issues/549), and until that lands a red check is
not a state this system can hold.

**It still serves this machine and nowhere else.** The briefing carries more than a listing of names
did, so the wall matters more than it did. Nothing here widens it. The wall, the three things a wider
front door would need, and the decision behind them are
[#550](https://github.com/atlantic-blue/quay-crew/issues/550), which landed first, and the refusal it
wrote is what this page sits behind.

See [#547](https://github.com/atlantic-blue/quay-crew/issues/547).
