# proving: the assumption that would waste the most work, proved where it has to hold

Every design rests on something nobody has run yet. Name that thing before you design around it, and
prove it in the runtime that will have to run it.

## What happened

A product was built to fetch captions from an AWS Lambda function. The hard part looked like the
attestation token, and bound to the video id the caption endpoint returned 406,491 bytes. That
measurement is real. It was taken on a laptop, on a home connection.

Deployed, the same fetch could not read a title out of the watch page at all. From the laptop the live
test read a page of 24,992 bytes, 19,420 characters across 560 segments. From the deployed function
the same video id returned the page saying there is no video with that id.

The assumption held everywhere it was tested and failed in the only place it had to hold. Two days of
product sat on top of it.

## Do this before you design

1. **Name the assumption that would waste the most work if it is false.** One sentence. It is usually
   the interesting part, the reason the thing is worth building at all.
2. **Write the narrowest thing that answers it.** Not the product, not a library, not a unit test. A
   few lines and one question.
3. **Run it in the runtime that will run the product.** The deployed function, the container image,
   the pod, the pipeline runner. Not this machine, and not a double.
4. **Write down what came back**: the number, the error, the size of the payload, and the date.

## Two traps, because both are what happened here

**A measurement taken somewhere easier is not evidence.** The network, the identity and the machine
are each part of the assumption. A data centre address, a role's credentials and a cold container are
each enough to turn the answer over.

**Working once feels like proved.** The assumption is the interesting part, so the first time it works
is the moment it feels finished. That is the moment to go and run it in the runtime, not the moment to
move on.

## What the design says

A design this skill reaches carries three lines near the top:

- **Riskiest assumption.** The one sentence.
- **Proved where.** The runtime the proof ran in, and when.
- **What came back.** The measurement.

**A proof run anywhere other than the target runtime is not yet proved, and the design says so in
those words.** A design carrying "not yet proved" is honest and can still be built on, because the
reader knows what to check first. A design that quietly reads as proved cannot.

`method.md` in this directory says how to write the narrow proof, what counts as the same runtime, and
what to do when the runtime does not exist yet. Read it when you are about to run one.
