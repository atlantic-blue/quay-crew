**Every row of the console's stats view says how that part of the crew is.** The view had six rows
and one thing to say about each of them: what it was configured to be. So it drew every row ready,
including the event log that had been dead for sixteen hours while the crew served on
([#445](https://github.com/atlantic-blue/quay-crew/issues/445)). A row that reports a dead component
without a state hides it.

The crew's health probe already wrote to the store and to the event log to decide whether it was
serving, and it threw away everything except the verdict, into a container's log. It now keeps what
each write found, and `GetHealth` answers with it. A row reads `serving`, `down`, `not configured`
for an event log nothing is connected to, or `not checked`, and it is drawn in the colour of the
word.

`GetHealth` answers from the last probe and never probes on the call, for the reason `GetHeadroom`
answers from the last sample: a broker that is down costs the whole export budget on every write, so
a view that probed when it drew would stall for exactly as long as the thing it reports on stays
broken.

Four of the six rows read `not checked`, because nothing probes the model backend, the sandbox
engine, the secrets store or where state is kept. That is what they are. Drawing them green would be
the same claim the events row made for sixteen hours, and probing them changes what serving means,
which is a separate decision.

The numbers [#458](https://github.com/atlantic-blue/quay-crew/issues/458) asks about are not here.
This view answers whether anything is broken and is read at a glance; how many sessions and jobs
there are, what they spent and what that cost is read while planning, and it wants a view of its own.
