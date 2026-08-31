**The verifier is given a method for telling a green check from a real one.** The role already
promised this. Its summary says it checks that a slice is wired in and not only that its tests are
green, and the brief gave a session no way of telling one from the other, so the answer came out of
whatever the model already believed about tests.

Two changes went past this crew's own checks that way. The `infrastructure` check passed in eleven
seconds: it runs a validate and a format check, and neither one talks to the cloud account. The
change merged and the deploy failed on its first write. A page collapsed every response it could not
read into one confident sentence, and nothing tested the case it could not identify, because that
case had no name. Every check was green, and none of them could have failed.

So [`roles/verifier/ROLE.md`](roles/verifier/ROLE.md) now asks one question of every change. If the
behaviour this change produces broke where it is used, would verification fail? Under it are the
three shapes a gap takes: the changed code regresses where it is used and no test covering that use
would fail, a site that should now use the new behaviour does not, or a test appears to cover the
behaviour and would not protect it because it is skipped, does not run in the normal path, or asserts
on something the break leaves unchanged.

Four things do not count as a test at all: one that runs the changed code and never checks the
changed result, one that mocks away the integration the change is about, a check that only asserts
that no error was thrown, and an assertion against source text rather than against a run. Every gap
carries a file, a line, which shape it is, what a person loses, and the search that grounds it. A gap
the session cannot ground is dropped rather than reported.

The role goes to version 2, so a session already running as the verifier keeps the brief it started
with, and a workspace moves by attaching again. Nothing came out of the file to make room: it was
9,037 bytes and it is 13,753, against a ceiling of 16,384.

The method is a rewrite of a file published by `bmad-code-org/BMAD-METHOD` under the MIT licence.
[`docs/ROLE-IMPORTS.md`](docs/ROLE-IMPORTS.md) records what was read and why, and the notice and the
address are in the brief itself, where a reader of the role reads them.

What this does not do. It is prose, so nothing holds a session to it: the system has no word for a
file, so it cannot stop the verifier changing one, and the brief says so at the top. It judges tests
and not the change's own description, which is
[quay-crew#534](https://github.com/atlantic-blue/quay-crew/issues/534) and lands in the same file
next.
