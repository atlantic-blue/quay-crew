**A job says what a person does with what it builds, and what they get back.** One sentence, in that
person's words. State it on the job at the top with `krewe job create --product "paste a link and get
the text back"`. `krewe job show` prints it, and every job declared under that one carries the same
sentence.

The session doing the job is given the sentence above its brief, and told that it wins. A session
that finds the sentence and the design disagreeing is asked to say so in its answer rather than to
build the design as written.

The reason. A tree of jobs built a design document faithfully and delivered it complete. Every check
was green. The operator opened it two days later and could not use it: `docs/ACCEPTANCE-PROJECT.md`
section 3 said the address reads `/videos?id=<video id>`, so every job downstream took the video
identifier as the key, and a reader holding a link had to dig that identifier out by hand. Nobody had
written the sentence a person would say, so nothing measured the address shape against anything.

A job declared under another inherits its parent's sentence. One that states a different sentence is
refused, naming the parent's, because a tree with two products has none. Under a job that carries
none, the new job's own sentence stands, so a tree can still gain one. A job at the top that states
nothing is not refused: the tool says the sentence is missing and how to write it, the way it already
says which skills a session starts without.

This is the first half of [#520](https://github.com/atlantic-blue/quay-crew/issues/520). The second
half, a run that stops at the first thing a person can open and asks whether it is what they wanted,
is in the entry beside this one.
