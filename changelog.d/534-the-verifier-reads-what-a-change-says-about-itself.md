**The verifier reads what a change says about itself, and tries to break it.** A pull request body, a
commit message and a code comment are the author telling you what they meant to write. None of the
fifteen roles read that prose against the code it describes, so a run took the author's account of
the change as a finding about the change.

The page that answered "No video with that id" for a video that exists shipped with a description
saying the page reports what it could not read. That sentence is a claim about the world, and nothing
tested it.

So [`roles/verifier/ROLE.md`](roles/verifier/ROLE.md) now carries a claims check under the tracing
method [#533](https://github.com/atlantic-blue/quay-crew/issues/533) gave it. The narrative is
testimony rather than evidence, and a claim repeated in a comment is the same claim a second time.
The session pulls out each checkable claim, what the change does, what it preserves, the order of two
things, the arithmetic, any claim of parity with code that already exists, then tries to falsify each
one against the code it already traced. A rendered sample shown as observed output is a claim too,
which is this crew's own rule 45 and had nowhere to live before.

A falsified claim is reported with the file and the line where the code contradicts it, the claim
quoted, what the code does instead, and what goes wrong for a person who believed it. **A claim the
session could not break produces nothing.** That half is the one a check like this loses first: a
session that returns every claim it read passes any test of whether a false claim is caught, and is
a report nobody opens.

The claims are read last. The tracing finishes first, so a claim cannot steer the trace that would
have caught it, and the section sits after the tracing method rather than before it for the same
reason.

The role goes to version 3, so a session already running as the verifier keeps the brief it started
with, and a workspace moves by attaching again. Nothing came out to make room: the brief was 13,753
bytes and it is 15,836, against a ceiling of 16,384. That leaves 548, so it is now the third of the
fifteen with no room left, and the next edit to it has to take something out.

The check is a rewrite of a file published by `bmad-code-org/BMAD-METHOD` under the MIT licence.
[`docs/ROLE-IMPORTS.md`](docs/ROLE-IMPORTS.md) records what was read and why, and the notice and the
address are in the brief itself, where a reader of the role reads them.

What this does not do. It is prose, so nothing holds a session to it: krewe has no word for a file,
so it cannot stop the verifier changing one, and the brief says so at the top. Nothing enforces the
order either. What ships is the instruction to read the claims last and the position of the section,
and the tests read both.
