**A failure nobody read is no longer reported as the most convenient known cause.** A deployed page
answered "No video with that id" for a video that was there and had captions. The code fetched the
watch page, could not find a title in it, and threw the one failure it knew the name of. A consent
wall, a refusal, a changed page and an empty body all arrive as a page with no title. Nothing was
logged at that boundary, so an hour later what the platform had returned was still unknown, and the
404 the operator read was a guess wearing a status code.

This is a shape rather than a defect, so it ships as a skill. `skills/outbound` says three things and
asks for a test: log the address, the status, the size and enough of the body to tell one refusal from
another, and never the credential; report a failure you did not recognise as unknown rather than as
the known one that happens to fit; and carry that distinction through to the page, the reply or the
error, because a wrong confident message sends the reader to the wrong place.

It is given to the system on a fresh install, beside git and github, so a session writing its first
call to another service holds it without anybody attaching anything. It names no secret and no binary,
which is what stops it being left out of the workspace that has set nothing up yet.
