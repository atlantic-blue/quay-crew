**Every length cap in this system is named, with what it became.** A change that lifts the caps one
at a time leaves nobody able to say which ones it lifted. The operator who accepts the work has to
read the source to find out. A cap nobody wrote down is a cap that still refuses text next month.
So the change carries this list.

The list is mechanical. A scan reads every Go file a person wrote here. It keeps every constant this
system compares against the length of something. It found 55 on 2 September 2026.

The scan is wide. It also finds constants that count rather than measure: how many attempts a loop
takes, and which cell of a row holds a value. Those get a line too, and the line says why the number
stays.

Each cap carries one of three words. A cap marked warning keeps the text whole. A line above the
text says which field is long, how many bytes it is, and what the guide is. A cap marked cut for
display cuts what is drawn and never what is stored. A cap marked kept still refuses, and the line
says what demands the number.

Three of the cuts run on the way into a store today rather than at the drawing: `labelLimit`,
`descriptionLimit` and `AttemptLimit`. They move to the drawing with the verticals beside this one.

This pull request carries the list. The code for each marking ships in the verticals beside it.

## The caps that become a warning

- `TellingLimit` in `internal/job/asking.go`, 16384 bytes: warning. It is an answer a person types
  for a session. The reader no longer measures an answer against it.
- `HandoffLimit` in `internal/job/ceiling.go`, 4096 bytes: warning. It is each half of a handoff to
  a fresh session.
- `ClaimLimit` in `internal/job/claim.go`, 200 bytes: warning. It is a job's hold on a piece of work
  in the world. The declaration no longer measures a claim against it.
- `DesignLimit` in `internal/job/design.go`, 3000 bytes: warning. It is the whole list of verticals
  as one question.
- `DesignLineLimit` in `internal/job/design.go`, 200 bytes: warning. It is one vertical on one line.
- `DesignVerticals` in `internal/job/design.go`, 7 verticals: warning. A longer list is a list a
  person reads and accepts, not a job the system stops.
- `EvidenceLimit` in `internal/job/evidence.go`, 200 bytes: warning. It is the label on a picture.
- `StepsLineLimit` in `internal/job/evidence.go`, 200 bytes: warning. It is one step of evidence.
- `IdeationLimit` in `internal/job/ideation.go`, 3000 bytes: warning. It is the whole reading as one
  question.
- `IdeationLineLimit` in `internal/job/ideation.go`, 200 bytes: warning. It is one question or one
  point of a reading.
- `IdeationPoints` in `internal/job/ideation.go`, 5 lines: warning. A sixth thing a session was told
  is worth reading.
- `IdeationQuestions` in `internal/job/ideation.go`, 5 questions: warning. A sixth question is worth
  reading.
- `UnderstandingLimit` in `internal/job/ideation.go`, 600 bytes: warning. This is the cap that
  stopped job a3d72b11 over a correct reading of 859 bytes.
- `BriefLimit` in `internal/job/job.go`, 16384 bytes: warning. It is the whole brief a job is given.
- `LabelCount` in `internal/job/job.go`, 16 labels: warning. A job carrying a seventeenth label is
  still a job.
- `ProductLimit` in `internal/job/job.go`, 200 bytes: warning. It is the one sentence a job serves.
- `TitleLimit` in `internal/job/job.go`, 200 bytes: warning. It is the guide almost every one line
  field in this system takes.
- `PlanStepLimit` in `internal/job/plan.go`, 200 bytes: warning. It is one step of a plan. The
  reader no longer measures a step against it.
- `PlanSteps` in `internal/job/plan.go`, 7 steps: warning. An eighth step is a plan a person reads,
  not a job the system stops.
- `RequestLimit` in `internal/job/request.go`, 16384 bytes: warning. It is what a person asked for,
  in the words they asked in. The declaration no longer measures it.
- `StepLimit` in `internal/job/resume.go`, 200 bytes: warning. It is one step a job records.
- `StepCount` in `internal/job/resume.go`, 40 steps: warning. A job that records a forty first step
  says where it got to.
- `SteerLimit` in `internal/job/steer.go`, 200 bytes: warning. It is what the operator had to say.
- `SummaryLimit` in `internal/hook/hook.go`, 200 bytes: warning. It is the line a listing of hooks
  shows.
- `BriefLimit` in `internal/role/role.go`, 16384 bytes: warning. It is the whole instruction a role
  gives one session.
- `SummaryLimit` in `internal/role/role.go`, 200 bytes: warning. It is the line a listing of roles
  shows.
- `BriefLimit` in `internal/skill/skill.go`, 4096 bytes: warning. It is the whole instruction a
  skill gives.
- `SummaryLimit` in `internal/skill/skill.go`, 200 bytes: warning. It is the line every session
  holding the skill pays for.
- `Limit` in `internal/repository/repository.go`, 200 bytes: warning. It is a repository address,
  and a long one is usually a paste of something else.

## The caps that become a cut for display

- `claimWidth` in `cmd/krewe/job.go`, 28 characters: cut for display. The claim shares a line with
  the title.
- `reasonWidth` in `cmd/krewe/job.go`, 40 characters: cut for display. It is why a job ended, on
  the row of `krewe job list`, and the record keeps the whole of it.
- `Limit` in `hooks/prose-gate/main.go`, 5 findings: cut for display. The gate says how many more
  there are, and reports them on the next attempt.
- `descriptionLimit` in `internal/controlplane/describe.go`, 60 characters: cut for display. A
  description shares one column with nine others.
- `labelLimit` in `internal/controlplane/server.go`, 60 characters: cut for display. A label shares
  one column with nine others.
- `detailLine` in `internal/controlplane/sessionevents.go`, 240 bytes: cut for display. It is one
  line about one event, and the task record holds the whole reply.
- `shortIDLength` in `internal/display/display.go`, 8 characters: cut for display. Actions take the
  whole identifier.
- `QuestionLimit` in `internal/job/asking.go`, 4096 bytes: cut for display. A question is read in a
  terminal, and the record keeps the whole of it. The reader no longer measures a question against
  it. It is the width a surface draws, and the surfaces that draw one line cut there and say so.
- `AttemptLimit` in `internal/job/loop.go`, 4096 bytes: cut for display. It is what the record keeps
  of one attempt, and the measure reads what is kept.
- `readReason` in `internal/job/pullrequest.go`, 200 bytes: cut for display. A refusal is read in a
  listing beside the address.
- `QuestionRowLimit` in `internal/job/question.go`, 200 bytes: cut for display. It is one row of a
  question in a terminal.
- `lineWidth` in `internal/telling/telling.go`, 80 characters: cut for display. Eighty columns is
  the narrowest terminal anybody still uses, so the line is cut there and says which command prints
  the whole question. It was `tellingWidth` at 60, which measured the question alone.

## The caps that are kept

- `MaxSentences` in `hooks/prose-gate/rules.go`, 6 sentences: kept because the Simplified Technical
  English standard sets the number, and this gate exists to hold prose to that standard.
- `MaxWords` in `hooks/prose-gate/rules.go`, 25 words: kept because the same standard sets that
  number for a description.
- `phaseColumn` in `internal/console/jobs.go`, cell 1: kept because the table this reads puts the
  phase in that cell. It measures no text.
- `permissionColumn` in `internal/console/resources.go`, cell 5: kept because the session row puts
  the mode in that cell. It measures no text.
- `projectColumn` in `internal/console/resources.go`, cell 2: kept because the session row puts the
  project in that cell. It measures no text.
- `ancestryLimit` in `internal/controlplane/steer.go`, 64 parents: gone. A job cannot be under
  another job, so there is no chain of parents to walk and no cycle to bound.
- `BuildAttempts` in `internal/job/build.go`, 2 attempts: kept because every ask is a task somebody
  pays for. It counts workers, not bytes.
- `StepsAtLeast` in `internal/job/evidence.go`, 2 steps: kept because it is a floor. It refuses one
  line wearing a number, which is the opposite of refusing length.
- `LabelLimit` in `internal/job/job.go`, 63 characters: kept because Kubernetes caps a label value
  at 63 characters, and a longer one is refused by the cluster.
- `LoopAttempts` in `internal/job/loop.go`, 3 attempts: kept because three attempts are what makes a
  loop a loop. It counts attempts, not bytes.
- `ShingleWords` in `internal/job/loop.go`, 3 words: kept because the similarity measure is defined
  on runs of three words.
- `TestAttempts` in `internal/job/test.go`, 2 attempts: kept because every ask is a task somebody
  pays for. It counts workers, not bytes.
- `shortestSecret` in `internal/model/redact.go`, 12 bytes: kept because a value shorter than that
  is far more likely to be a setting than a credential.
- `MaxLength` in `internal/name/name.go`, 64 characters: kept because a person types a name as half of an
  address. It is also a directory name on disk.
- `reasonWords` in `internal/promise/promise.go`, 3 words: kept because it is a floor. It makes it
  impossible to say nothing.
