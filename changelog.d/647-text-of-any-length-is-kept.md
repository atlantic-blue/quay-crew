**Text a person writes is kept word for word, at whatever length the work needs.** Six length caps
refused the text whole. A job wrote a correct reading of 859 bytes against a ceiling of 600. The
system asked it once more, and the second reply stopped the job for good. Ten million tokens were
spent, and the advice the job left was to write a different brief.

The brief, the request, the claim, a question, an answer and a step of the plan now refuse nothing
for their length. Each number stays as a guide to what a reader takes in. Each one says in the source
that it is a guide rather than a ceiling:

- `BriefLimit` in [internal/job/job.go](internal/job/job.go), and `RequestLimit` in
  [internal/job/request.go](internal/job/request.go), which is the same number
- `ClaimLimit` in [internal/job/claim.go](internal/job/claim.go)
- `QuestionLimit` and `TellingLimit` in [internal/job/asking.go](internal/job/asking.go)
- `PlanStepLimit` in [internal/job/plan.go](internal/job/plan.go)

The plan is the one that cost a job rather than a field. A step over the number made the whole reply
unreadable to the system. The system then asked for the plan again, and a second long reply stopped
the job. A long step is now read, kept, and put on the row a person approves.

`krewe job show` reads the answer back too. The row kept what a person answered, and the tool printed
it. The wire between the two dropped it. The `Job` message carries a `told` field and nothing filled
it in, so an answer of any length read back as nothing at all.

The steer goes with them. `krewe steer "..."` refused anything over 200 bytes, so the operator with
most to say was the one the system would not hear, and the mark was lost with the words.
`SteerLimit` in [internal/job/steer.go](internal/job/steer.go) is a guide now. The record keeps every
word. The report draws one line for each steer, so it cuts there and says which command prints the
whole of it.
