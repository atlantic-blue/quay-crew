**A job that designs something is told to prove its riskiest assumption where it has to hold.** A
product was built to fetch captions from an AWS Lambda function. The attestation token looked like the
hard part, and bound to the video id the caption endpoint returned 406,491 bytes. That measurement was
taken on a laptop. Deployed, the same fetch could not read a title out of the watch page at all: the
runtime got back the page saying there is no video with that id. The assumption held everywhere it was
tested and failed in the only place it had to hold, with two days of product already sitting on it.

The `proving` skill says the rule: name the assumption that would waste the most work if it is false,
run the narrowest thing that answers it in the runtime that will run the product, and write into the
design what came back and where it ran. A proof run anywhere else is stated as not yet proved, in those
words. `method.md` beside the brief says what counts as the same runtime, which is the network, the
identity, the image, the limits and the region, and what to do when the runtime does not exist yet.

A fresh system takes it at the system level, beside git and github, so a job that designs something is
offered it without anybody attaching anything. It names no secret and no binary, so nothing can leave
it out of a session, and it costs what any skill costs: one line in the memory file, with the brief on
disk until the model opens it.
