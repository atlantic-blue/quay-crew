**Every command says when the crew is reporting itself not serving, and names the part that is
down.** A crew's event log had been dead for sixteen hours
([#445](https://github.com/atlantic-blue/quay-crew/issues/445)). Its health check had failed 1,467
times in a row, and the only thing reading that answer was a container health check nobody watches.
Every write still spent the whole export budget on a broker that was gone, every event went
nowhere, and the operator working through this tool all day read a crew that answered everything.

The crew already knew, and the tool never asked. It asks now, on every command that talks to the
control plane, and puts one line on standard error for each part the last probe found down:

    quay: this crew is not serving: events is down, so nothing it writes there lands. the event log
    did not take a record: unable to dial: lookup redpanda on 127.0.0.11:53: no such host

Standard error, and never an error, for the reason the build drift line uses both: standard output
is where a caller reads data, and a warning that refuses a command is worse than the crew it warns
about. It reads the crew's last probe rather than taking a fresh one, so it costs a call answered
from memory: a probe on the call would stall for exactly as long as the dead thing it reports on.

Only `down` is said. A crew with no event log configured is a real crew, and a part nothing probes
is the absence of a reading, so a line about either would print on every command forever and stop
being read. All four states are in the console's stats view, which is where somebody who is asking
looks; this is for the operator who is not.
