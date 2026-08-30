# The shape in code

Open this when you are writing the call, not before.

## The worked example this came from

`src/transcript/youtube.ts` fetched a watch page and read the title out of it. When the title was not
there it threw `VideoNotFound`, so the page answered "No video with that id" for a video that was
there and had captions. Nothing was logged at the fetch, so the only evidence anybody had afterwards
was the 404 the code had invented.

Both defects are one defect: a response nobody read, given the name of the failure that was easiest
to reach for.

## An outcome that can say it does not know

Three outcomes, not two. A boolean cannot carry the third one, and neither can a null.

    type Outcome<T> =
      | { kind: "ok"; value: T }
      | { kind: "absent" }                                    // the other end said so
      | { kind: "unknown"; status: number; body: string }     // we did not recognise this

`absent` is returned only where the answer said it: a documented status, an error code, an empty
result set from something that answers with one. Everywhere else the answer is `unknown`, and it
carries what would let somebody tell later.

The same shape in Go is a sentinel for the case you recognised and a wrapped error carrying the
status and the excerpt for the case you did not. The rule is the shape, never the language.

## One line, at the boundary

    log.info("fetched", {
      url: redacted(url),
      status: response.status,
      bytes: body.length,
      body: body.slice(0, 300),
      ms: elapsed,
    })

What each part is for: the address says which call this was, the status and the size say what class of
answer arrived (a 200 of 1,400 bytes is a consent wall where 90,000 bytes is the page), and the
excerpt is the only thing that tells a refusal from a redirect from a changed page.

## What never goes in the line

The authorization header, a bearer token, a session cookie, an api key in a query string, a password,
a signed url with its signature. Redact the whole value rather than trimming it: a truncated secret is
still a secret with a hint attached.

Redact the request before logging it, so nothing depends on remembering at the call site.

## The tests to write

Four responses, one test each, all against the unknown branch:

- a body in a shape that does not parse
- an empty body with a 200
- a redirect to a consent or login page
- a 200 that carries an error inside it

Each asserts two things: the outcome is unknown, and what the caller finally shows says unknown.
Stopping at the outcome leaves the honest result free to be rendered as the convenient one, which is
where this defect lived.

Watch each of them fail before you make it pass. A test of an unknown branch passes very happily
against code that has no unknown branch at all, because a thrown `NotFound` and a returned unknown
both leave the page saying something.
