**The front door stays on this machine, and the work reaches another device through a chat channel.**
The operator asked for the briefing on a phone. `krewe web` refuses every address that is not this
machine, and it refuses on purpose: the control plane listens behind one shared token, and this
server holds that token. A wider front door needs three things the system does not hold. A credential
for each device, so one phone is not every phone. A way to withdraw one device. A rule about
encryption on the path. Reaching a phone with a browser reverses a decision recorded in
[#302](https://github.com/atlantic-blue/quay-crew/issues/302), and that is a decision for a person.

It is decided, and it is written in the authentication section of
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): the door stays where it is, and the work travels through
a chat channel instead. A chat channel needs none of the three, and it does the thing a page cannot
do, which is speak first rather than wait for somebody to open it. That road is
[#9](https://github.com/atlantic-blue/quay-crew/issues/9) and
[#10](https://github.com/atlantic-blue/quay-crew/issues/10).

Nothing binds anywhere new and nothing sends a message. What changed is the refusal: it now names all
three things the system lacks and the road that was taken instead, so an operator who binds the wrong
address reads the decision rather than a wall. A scenario in [features/](features/) reads the three
out of the document and holds the refusal to them, because a reason that lives only in a code comment
drifts away from the document that decided it.
