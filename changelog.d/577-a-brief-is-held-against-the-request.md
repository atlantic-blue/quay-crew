**A job keeps the request that produced it, and the brief is read against it at the write.** A person
says one sentence. Something writes a brief from it. The system runs the brief faithfully and fast,
every check goes green, and nothing ever holds the brief up against the sentence, because the sentence
was never written down anywhere. Two of those this week. A request for an article about what had been
built became a brief for a diary of throughput. A request to paste a link and get the text became a
design whose address takes a video identifier, and a product was built from that design over two days.

The `product` sentence does not close this and could not. It says what a person does with what is
built and what they get back, which is an outcome, and on the article it read "a reader opens the
post" while the brief was a diary of throughput. Those two agree completely. So `request` is a field
of its own: what was asked for, in the words it was asked in, kept whole, never rewritten, and
inherited by every job under it.

The reading refuses nothing. It is the shape `left_out` already takes: an answer at the moment of the
declaration, naming the words of the request the brief never says, and empty where the brief says
them. A false alarm would stop work that was right, and a question to a person would be an approval on
every job, which is the cost the whole system exists to remove. The session doing the job is given the
request above its brief and told which of its words are missing, which is the half that works with
nobody watching.

The measure is the content words of the two texts, so it costs no model call and anybody holding the
row can work it out again. The threshold is two thirds, measured rather than chosen: on the 27 pairs
in this build of a summary beside the brief written to serve it, the lowest covers 0.778 and the median
covers 1.000, while the two briefs that cost real work cover 0.500 and 0.000. The measurement runs on
every build in `internal/job/requestcalibration_test.go`.

What it does not catch is said in the code and in the design: a brief that keeps every word and
inverts the meaning reads as faithful, and a brief that renames a thing reports the rename as a
dropped word. On a looser corpus of 121 issue titles held against their bodies, one in eight falls
below the threshold, which is the price of the check and it is one line of text.
