**A run that builds something a person can open stops once, at the first usable path, and asks
whether it is what they wanted.** A graph says which step builds that thing with `usable: true`, and
says what the run serves with a `product:` line beside its name. When that step lands, the run stops
and puts one question: here is the address, here is the sentence, does this do what the sentence
says.

The answer of no does not end the run. Anything but `yes` is read as the sentence the operator wanted
instead, and it replaces the one on the job carrying the run before the next step is declared, so
every step after it is done against the new sentence and every session doing one is given it above
its brief. An answer of no at the first usable path costs one step. The same answer once everything
is built costs the run.

The reason. A tree of jobs built `docs/ACCEPTANCE-PROJECT.md` section 3 faithfully and delivered it
complete. Every check was green. The operator opened it two days later and could not use it: the
document said the address reads `/videos?id=<video id>`, so a reader holding a link had to dig that
identifier out by hand. Every job did what it was asked. What no run had was a step where a person
sees a usable thing early enough for an answer of no to be cheap.

Refused at import, because a refusal in the middle of a run arrives hours later with nothing pointing
back at the file: a graph that marks two nodes, because a run stops once; a node that is not a
dispatch, because only a dispatch builds anything; and a graph that stops for a person and declares
no sentence, because the question would then name an address and nothing to measure it against. A
step that built something and replied with no address stops the run rather than asking a question
naming something nobody can open.

This is the second half of [#520](https://github.com/atlantic-blue/quay-crew/issues/520). It is the
flow engine's, so it reaches a tree of jobs only where a flow runs one: a job that declares its
children directly still has nowhere to stop. No graph in [flows/](flows/) marks a step yet, because
none of the three builds a first usable path.
