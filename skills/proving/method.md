# How to prove one assumption in the runtime

Read this when you are about to run a proof. The rule itself is in `SKILL.md`.

## The narrowest thing that answers the question

One job, one question, one answer written down. It is not the product, and nothing built for it
survives the proof: the answer survives, the code goes.

Keep it under an hour. If a proof needs a day, the thing being proved is more than one assumption, so
split it and prove the one that would waste the most work.

Ask for the smallest fact that decides it. Not "does the fetch work" but "does this identity, from
this network, get more than a hundred bytes of captions for this video id". A question with a number
in the answer cannot be read two ways later.

## What counts as the same runtime

The runtime is the whole of it, not the language. Each of these has turned a proof over on its own:

- **The network.** A data centre address is treated differently from a home connection by anything
  that rate limits, geolocates or blocks. This is what the captions failure was.
- **The identity.** The role, the token or the service account the deployed thing carries, never the
  credentials on your machine.
- **The image.** The binaries, the certificate store, the fonts, the shared libraries, the version of
  the language. A tool that is on your machine is not in a distroless image.
- **The limits.** Memory, timeout, disk, concurrency, and the cold start that comes before the first
  request.
- **The region.** Latency and the presence of a service both change with it.

If any one of those differs from the deployed thing, the proof ran somewhere easier and the design
says not yet proved.

## When the runtime does not exist yet

Stand up the smallest version of it, and let that be the first slice of the work. An empty function
with the spike as its handler, deployed by the pipeline that will deploy the product, is a day at
most and it proves two things at once: the assumption, and that the pipeline reaches the runtime.

Never let the answer be "we will find out when we deploy". That is the failure this skill exists to
end, and it costs whatever was built in the meantime.

## What to write down

In the design, next to the assumption:

- The runtime, named exactly: the function, the image tag, the cluster, the pipeline job.
- The date it ran.
- The command or the request, so somebody else can run it again.
- What came back, as a number or an error, quoted rather than summarised.

Write the failed proofs down too. An assumption that failed once and passed after a change is a
constraint the design has to carry, and the reader cannot infer it from the passing run alone.

## A worked example

**Assumption.** A deployed function can read the captions for a video id.

**Proof, done wrong.** The fetch ran on a laptop, on a home connection, and returned 406,491 bytes.
Two days of product were built on it.

**Proof, done right.** Deploy an empty function that fetches one hard coded video id and returns the
byte count and the first 200 characters. Invoke it. From the deployed function the same video id
returns the page that says there is no video with that id, so the assumption is false in the runtime
and the design needs a different way to fetch, an hour into the work rather than two days in.
