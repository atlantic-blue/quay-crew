# outbound: what came back, and what you are allowed to call it

A deployed page answered "No video with that id" for a video that was there and had captions. The
code fetched the watch page, could not find a title in it, and threw the one failure it knew the name
of. A consent wall, a refusal, a changed page and an empty body all arrive as a page with no title,
and nothing was logged at that boundary, so an hour later what the platform had actually returned was
still unknown. The 404 was a guess wearing a status code, and the operator believed it.

This is about every call that leaves the process: another service, a page, a queue, a database, a
command line tool, a model. Three rules, and a test.

## Log what came back

At the boundary, before anything parses it: where the call went, the status, the size in bytes, and
enough of the body to tell one refusal from another. A few hundred characters is usually enough, and
one line is enough. Log it whether the call worked or not, because the failures worth reading are the
ones nobody predicted.

Never log the credential. Not the authorization header, not the token in the query string, not the
cookie. Whoever reads the logs is not always whoever is trusted with the key.

## Never name a failure you did not read

There are two kinds of failure and they get different names.

One you recognised, because something in the answer says so: a status, a documented error code, a
body you matched. Report it by its name.

One you did not recognise. Report it as unknown. "The platform answered something we did not expect"
is honest. "This video does not exist" is a claim about the world, and it is the wrong one whenever
the answer was a consent wall.

The trap is that the convenient known cause is always available. A parse that finds nothing is
evidence that the parse found nothing. It is never evidence that the thing is absent: absence is
something the other end has to say.

## Carry it through to what the person sees

The distinction is worth nothing if it dies inside the code. The page, the reply, the exit code and
the error all say unknown when the outcome is unknown, and they carry what you logged: the status,
and enough of the answer for somebody to act on it.

A wrong confident message costs more than a vague one. It sends the reader to look in the wrong
place, and it spends the trust that makes the right messages worth reading.

## Test the unknown case

Ship a test for the answer nobody planned for, in the same change as the call. The recognised
failures usually get one. The unknown branch is the one that gets skipped, and it is the branch that
lied.

Give it a body in a shape that does not parse, an empty body, a redirect to a consent page, and a 200
that carries an error inside it. Assert the outcome is unknown, and assert that what the caller or
the page finally shows says so.

`shapes.md` in this directory has the shape in code, what to redact, and the worked example this
brief opens with.
