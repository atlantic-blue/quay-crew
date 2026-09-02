**A long reading warns the person and keeps every word.** The guides on what a job says it understood
refused the text. Job a3d72b11 wrote a correct reading of 859 bytes against a guide of 600. The
system threw the reply away, asked the session once more, and stopped the job on the second reply.
Ten million tokens were spent. Nothing reached the person, and the only thing that job produced was
the text nobody read.

`UnderstandingLimit`, `IdeationLineLimit` and `IdeationLimit` in
[internal/job/ideation.go](internal/job/ideation.go) are guides now. A part of the record that is
longer than its guide is kept word for word. A line above the record says which part is long, how
many bytes it is, and what the guide is, so the operator can say "that is fine" or "say it shorter
next time".

The counts are unchanged. A reply may still ask at most five questions and carry at most five lines
under each heading, and a question that asks whether to go on is still refused.
