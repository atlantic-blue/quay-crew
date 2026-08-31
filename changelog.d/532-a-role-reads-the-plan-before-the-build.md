**A role reads the plan before anybody builds it.** A run reads a design document and builds it.
Nothing read a design document and asked whether it held together. The crew delivered the
transcript product complete, every check was green, and the operator opened it two days later and
could not use it, because section 3 of the document said the address carries a video identifier
and nobody had written down that a person arrives holding a link
([#520](https://github.com/atlantic-blue/quay-crew/issues/520)).

`plan-critic` ships in [`roles/`](roles/) as the sixteenth role. It reads the design, the
contracts and the build order before any code exists, against the one sentence the job carries
([#523](https://github.com/atlantic-blue/quay-crew/pull/523)). It runs on opus, receives `job`,
`context` and `skills`, and declares no verbs, so it may call nothing. Its answer is the report.

Seven classes of finding. Six are imported from
[github/spec-kit](https://github.com/github/spec-kit), which is MIT licensed, copyright GitHub,
Inc.: duplication, ambiguity, underspecification, conflict with the declared standards, coverage
gaps and inconsistency. The seventh is ours, because the source checks a plan against itself and
never asks whether the plan is the right product: a requirement the plan does not trace to the
sentence. The brief records the licence and both files it was read from, and `krewe role show`
prints the brief, so a reader of the role finds them without leaving it.

The sharper half of the import is the rule about what a finding may be. The source calls it "unit
tests for English": test the requirements, not the implementation. Wrong, "the page shows three
cards". Right, "is the number of cards written down". Applied to the run that failed, "is it
written down what a person types and what they get back", which was answerable from the document
alone, months before anybody could open the page.

Every finding names where it is, and a plan that holds up gets one line saying so. A critic that
reports something every time is a critic every run learns to skip, and one that refuses everything
stops every run.

Two things this does not do. No test proves the role finds a real defect in a real plan, because
the finding is the model's job; what is held is the manifest, the seven classes and the rules in
the brief the session is actually handed, read back off the memory file in its container and out
of the database. And nothing runs it: no flow graph names it, and an operator declares the job or
writes it into a graph, which is true of all sixteen.
