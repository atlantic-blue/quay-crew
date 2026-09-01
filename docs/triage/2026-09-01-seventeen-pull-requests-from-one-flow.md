# Seventeen pull requests nobody wanted, from one flow, on 1 September 2026

## What ran

The `pull-request-land` flow ran against `atlantic-blue/transcript`. The operator ran it eight times,
in three versions of the graph. Its work was to carry pull request 4 from conflicting to landed.

The flow file is not in this repository. It is not on `main`, and it is on no branch here. Issue 594
says the same thing. So this record reads the pull requests the flow left behind, and not the graph.

## What it produced

Pull request 4 never landed. The flow opened seventeen pull requests of its own, numbered 5 to 21.
Each one added a document. The document records what a step read. Each one sat on a branch of its
own. The operator closed all seventeen by hand and deleted every branch.

The numbers below come from the forge on 1 September 2026:

```
gh pr list --repo atlantic-blue/transcript --state all --limit 30 \
  --json number,createdAt,mergedAt,files
```

Seventeen opened. None merged. Ten came from the step that picks the subject. Seven came from the
step that reports what landing found.

```
 5  00:57:28  subject.md
 6  07:12:02  subject.md
 7  07:13:55  subject.md
 8  07:23:54  land.md
 9  07:33:03  docs/subject.md
10  07:44:42  docs/land.md
11  07:51:55  docs/the-subject-to-land.md
12  07:58:59  docs/landing-pull-request-4.md
13  08:04:55  docs/subject-pull-request-4.md
14  08:12:50  docs/land-pull-request-4-the-address-is-refused.md
15  08:20:20  docs/the-subject-is-pull-request-4.md
16  08:29:14  docs/land-pull-request-4-the-check-is-red-on-main.md
17  08:36:43  docs/subject-4-the-oldest-with-a-red-check.md
18  09:13:11  docs/subject-4-the-only-red-check-of-fourteen.md
19  09:23:34  docs/land-4-the-address-is-refused-and-the-code-is-proved.md
20  09:30:22  docs/subject-4-sixteen-open-and-one-red-check.md
21  09:39:01  docs/land-4-the-refusal-is-live-and-older-than-the-branch.md
```

Sixteen of the seventeen arrived between 07:12 and 09:39, which is two hours and twenty seven
minutes. The busiest hour of that window holds eight. Nothing counted them, and nothing said a word
about them.

## The file names are the record of a workaround

Read the paths in order. The first three write `subject.md` at the root.
Pull request 6 says so itself. It reads:
"pull request 5 adds a file of the same name, so these two conflict with each other while neither
conflicts with main."

The path then moves to `docs/subject.md`. Then it becomes a longer name for each run. By pull request
21 the name carries the finding of that run.

Each change removed a conflict between two runs. No change asked why a step that reads the checks on
somebody else's pull request writes a file at all.

## Why a reading step wrote

A job that names a repository is not done until its answer names a pull request against that
repository. The rule is `internal/job/repository.go` on `main`. It is right, and it exists because a
three hour run once produced one readable thing at the end.

The project holds the repository. A job that names none takes the project's, in
`internal/controlplane/job.go`. A flow step is an ordinary job, declared in
`internal/flow/engine.go`, so every dispatch step of this flow named `atlantic-blue/transcript`.

The steps of this flow read. One picks the subject. One reports what landing found. Neither one
changes the code. Both were held to the rule, so both had to invent a change to finish.

The path is exact. The session answers. `PullRequestIn` finds no address. The controller sends
`AskedForThePullRequest`, which says the work is nowhere anybody can read it. The session writes a
document, opens a pull request, and answers with the address. The job settles.

## What the flow got right

Each pull request body is accurate. Pull request 20 reports that `gh pr checks 4` prints two green
lines and exits 0. It reports that the failing `plan and apply` check is visible only on the head
commit. The deploy workflow does not start on a pull request, so the forge keeps that run out of the
summary. Pull request 21 reports that the platform answers the deployed function with LOGIN_REQUIRED.
It reports that all nine deploy runs failed, including the run on `main` before the branch existed.

That is good reading. The system had nowhere to put a reading, so it put each one in a pull request.

## The measurement that shapes the guard

A ceiling on pull requests per repository per hour cannot separate this from real work.

On `atlantic-blue/quay-krewe` the busiest hour opened fourteen pull requests. That number comes
from 356 pull requests, read on 1 September 2026. A program measured it. It slides a one hour window
over their creation times. It opens one window at each pull request. Twenty four of those windows
hold ten or more.

```
gh pr list --state all --limit 700 --json number,createdAt
```

So a ceiling low enough to catch seventeen from one flow refuses a normal day on this repository. The
count has to be per graph, which the job row already carries as the label `flow.graph`.

## What this record asks for

Issue 606 holds the fix and the guard. It is at
https://github.com/atlantic-blue/quay-krewe/issues/606.
