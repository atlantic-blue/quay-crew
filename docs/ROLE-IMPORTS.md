# What to import from published roles

The acceptance run failed in six ways. Six issues record them. This document asks one question about
each failure: does somebody already publish a role definition that would have caught it, and may we
lawfully copy it.

I read the definitions themselves, not the pages that describe them. Every address below was fetched
on 30 August 2026. Every licence below was read from the repository's own licence file, not taken
from the summary the interface prints.

## The shortlist

Ranked by the failure it prevents, hardest first.

1. **A new role, `plan-critic`.** It reads the design, the contracts and the build order before any
   code exists, and reports where they disagree and what they do not answer. Prevents
   [#520](https://github.com/atlantic-blue/quay-crew/issues/520). Import the method from
   [github/spec-kit](https://github.com/github/spec-kit), licence MIT. Work:
   [#532](https://github.com/atlantic-blue/quay-crew/issues/532).
2. **Sharpen `verifier` with the three shapes of a verification gap.** A green suite is evidence only
   where an assertion would fail if the behaviour broke. Prevents
   [#519](https://github.com/atlantic-blue/quay-crew/issues/519) and
   [#522](https://github.com/atlantic-blue/quay-crew/issues/522) from returning as a green check.
   Import from [bmad-code-org/BMAD-METHOD](https://github.com/bmad-code-org/BMAD-METHOD), licence
   MIT. Work: [#533](https://github.com/atlantic-blue/quay-crew/issues/533).
3. **Sharpen `verifier` with a claims check.** The change's own description is testimony, not
   evidence, so each claim in it is falsified against the code. Prevents
   [#522](https://github.com/atlantic-blue/quay-crew/issues/522). Import from the same repository,
   licence MIT. Work: [#534](https://github.com/atlantic-blue/quay-crew/issues/534), which edits
   the same file as #533, so merge #533 first.

Three recommendations, one of them a new role. That is the whole shortlist.

## What nothing published covers

**Nobody puts a person in front of a usable thing early.** This is the second half of
[#520](https://github.com/atlantic-blue/quay-crew/issues/520) and the operator names it as the gap
that costs most. I found no published role definition for it. The practice is old and has two names,
the walking skeleton and Amazon's working backwards, and neither is published as a role a system can
import. It is also probably not a role at all. Issue #520 already says the mechanism: an `ask` node
at the first usable path, and `ask` is built. That is flow wiring and a clause in the orchestrator
brief. Writing a role for it would add a session that stops a run, where a node that stops a run
already exists. This is the finding that saves the most work.

**[#529](https://github.com/atlantic-blue/quay-crew/issues/529), a failed job starts again from
nothing.** No role fixes this. A resume needs the control plane to record which steps finished. A
brief cannot record anything.

**[#528](https://github.com/atlantic-blue/quay-crew/issues/528), the score is counted by hand.** Same
answer. A steer is a row and a command, not a way of working.

**[#521](https://github.com/atlantic-blue/quay-crew/issues/521) and
[#522](https://github.com/atlantic-blue/quay-crew/issues/522) are closed.** The `proving` and
`outbound` skills shipped. [#519](https://github.com/atlantic-blue/quay-crew/issues/519) shipped the
`deploy-identity` skill. Recommendation 2 and recommendation 3 defend those three skills against the
way they will decay, which is a check that passes without reading anything.

## Evidence

### 1. `plan-critic`

**Read at.**
[templates/commands/analyze.md](https://github.com/github/spec-kit/blob/main/templates/commands/analyze.md)
and
[templates/commands/checklist.md](https://github.com/github/spec-kit/blob/main/templates/commands/checklist.md).
I read `analyze.md` in full, 255 lines. I read the goal, the execution steps and the core principle
of `checklist.md`, which is about 150 of its 379 lines.

**Licence.** MIT, read at
[LICENSE](https://github.com/github/spec-kit/blob/main/LICENSE). The copyright holder is GitHub, Inc.
Quay is Apache 2.0, so MIT text may be copied with the notice kept.

**Which failure, and how.**
[#520](https://github.com/atlantic-blue/quay-crew/issues/520). The crew built the design document
faithfully. Nothing asked whether the document was the product. `analyze.md` runs one read only
pass over the specification, the plan and the task list before any implementation. It reports six
classes of finding: duplication, ambiguity, underspecification, conflict with the declared
principles, coverage gaps, and inconsistency. A coverage gap is a requirement with no task, and the
address shape in section 3 of `docs/ACCEPTANCE-PROJECT.md` had no requirement above it at all.

`checklist.md` carries the sharper idea, and it is the one worth taking. It calls its output "unit
tests for English" and states the rule as: test the requirements, not the implementation. Its own
example is the distinction the acceptance run missed. Wrong: "verify the landing page displays three
cards". Right: "is the exact number and layout specified". Applied to the transcript product, the
question becomes "is it written down what a person types and what they get back", and the answer was
no.

**Honest limit.** `analyze.md` checks the plan against itself and against a written constitution. It
never asks whether the plan is the right product. Half of #520 has no source, so we write that half.
The clause is short: the job now carries one sentence about what a person does and gets, from
[#523](https://github.com/atlantic-blue/quay-crew/pull/523), and `plan-critic` reads the plan against
that sentence.

**What it receives, and the model.** `receives: job, context, skills`. No `verbs`, so it may call
nothing. It reports and changes no file, the way `verifier` does. Model `opus`, because judging
whether a written plan answers a person's sentence is the expensive judgement, not a file written to
a specification.

**Why no existing role covers it.** `architect` writes the contracts, so asking it to review them
makes it the only reader of its own work. Quay already refuses that shape: `docs/ROLES.md` says a
second opinion that read the first opinion is not a second opinion. `assessor` reads an existing
codebase, and at this moment no code exists. `verifier` reads a finished slice against its contracts,
which is the same question one step too late. Nothing in the fifteen reads a plan.

**What we would not copy.** The file is a slash command for one tool, so about a third of it is hook
discovery and script paths. Its output format is markdown tables, which this crew does not write. The
method survives the rewrite. The prose does not.

### 2. Sharpen `verifier` with the verification gap shapes

**Read at.**
[src/bmm-skills/ship/bmad-build/review-prompts/verification-gap.md](https://github.com/bmad-code-org/BMAD-METHOD/blob/main/src/bmm-skills/ship/bmad-build/review-prompts/verification-gap.md).
Read in full.

**Licence.** MIT, read at
[LICENSE](https://github.com/bmad-code-org/BMAD-METHOD/blob/main/LICENSE). The copyright holder is
BMad Code, LLC. The same file adds a trademark notice over the names BMad, BMad Method and BMad Core.
The notice restricts the names and not the text, so we copy the text and use none of the names.

**Which failure, and how.** [#519](https://github.com/atlantic-blue/quay-crew/issues/519) and
[#522](https://github.com/atlantic-blue/quay-crew/issues/522). The file asks one question: if the
behaviour this change produces broke where it is used, would verification fail. In #519 the
`infrastructure` check passed in eleven seconds and never talked to the account, which is the file's
third shape, a broken verification gap. Its evidence rules are the crew's rule 44 written as
instructions a session follows: read a test before claiming what it covers, search the whole
repository by symbol before claiming no test exists, and say how far you looked. It also lists what
does not count as a test, and the list includes the two traps this crew keeps hitting, a test that
runs the code without checking the changed result, and a test mocking away the integration.

**What it receives, and the model.** No change. `verifier` keeps `receives: job, context, skills`, no
verbs, and model `sonnet`. This is a brief edit and a version increase.

**Why not a new role.** It is the same question `verifier` already asks. Its summary says it checks
that a slice is wired in and not only that its tests are green. The file gives that sentence a
method. A second verifying role would split one question across two sessions.

**The cost.** `verifier` is 9,037 bytes and the brief ceiling is 16,384, so it fits. The source file
is 9,505 bytes on its own, so the import is a rewrite to the shapes and the evidence rules, not
a paste.

### 3. Sharpen `verifier` with a claims check

**Read at.**
[src/bmm-skills/ship/bmad-code-review/references/claims-check.md](https://github.com/bmad-code-org/BMAD-METHOD/blob/main/src/bmm-skills/ship/bmad-code-review/references/claims-check.md).
Read in full. It is 1,362 bytes.

**Licence.** MIT, same file as above.

**Which failure, and how.** [#522](https://github.com/atlantic-blue/quay-crew/issues/522). The rule
is one sentence: the change's own narrative is the author's testimony, not evidence, and a claim
repeated in a code comment is the same claim rather than confirmation. The reader extracts each
checkable claim and tries to falsify it against code already traced. The transcript page answered
"no video with that id" for a video that exists. That message is a claim about the world, and nothing
tested it. The file also orders the work: read the claims last, after the tracing, so the claims
cannot steer the trace.

**What it receives, and the model.** No change to `verifier`.

**Why no existing role covers it.** `security` reviews a change for security defects. `verifier`
reads contracts and wiring. Neither reads what the change says about itself. This is also the crew's
rule 45, which no role states.

## What I read and rejected

- [wshobson/agents](https://github.com/wshobson/agents), MIT, read at
  [LICENSE](https://github.com/wshobson/agents/blob/main/LICENSE). I read
  [deploy-with-verification.md](https://github.com/wshobson/agents/blob/main/plugins/operating-kit/agents/deploy-with-verification.md)
  in full. One sentence in it is worth keeping: exited zero and live and serving the new code are
  different claims. The rest is a deploy runner, and this crew deploys through the pipeline on merge,
  so a role that runs a deploy contradicts rule 26. The sentence belongs in the `deploy-identity`
  skill, not in a role.
- [ui-visual-validator.md](https://github.com/wshobson/agents/blob/main/plugins/accessibility-compliance/agents/ui-visual-validator.md),
  first 60 lines of 192. Its first principle is good: assume the goal was not achieved until the
  picture proves it. Most of what I read after that is a catalogue of commercial testing products.
  The `browser` skill already holds this crew's version of the principle.
- [VoltAgent/awesome-claude-code-subagents](https://github.com/VoltAgent/awesome-claude-code-subagents),
  MIT. I read
  [project-idea-validator.md](https://github.com/VoltAgent/awesome-claude-code-subagents/blob/main/categories/10-research-analysis/project-idea-validator.md)
  first 90 lines of 269, and I listed all 158 names. The definition is lists of two word noun
  phrases under headings, for example "assumption destroying" and "bias elimination". A session
  cannot follow that. It also answers a different question, whether a market wants the product, and
  #520 is about whether the document describes the product. Rejected on substance, not on licence.
- [ruvnet/claude-flow](https://github.com/ruvnet/claude-flow), MIT. I read
  [production-validator.md](https://github.com/ruvnet/claude-flow/blob/main/.claude/agents/testing/production-validator.md),
  first 70 lines of 372. Its one idea, that no mock may remain and that the tests must run against
  real systems, is already `verifier` plus rule 44. What I read is mostly example TypeScript for
  another repository.
- [bmad-prfaq](https://github.com/bmad-code-org/BMAD-METHOD/tree/main/src/bmm-skills/plan/bmad-prfaq),
  MIT. I read `SKILL.md` and `references/verdict.md` in full. This is Amazon's working backwards
  method: write the press release for the finished product first. It is the closest published thing
  to the missing half of #520. I do not recommend importing it. It runs five interactive coaching
  stages with a human, it reads a configuration file, it runs a Python resolver, and it fans out its
  own subagents. Almost all of it is the framework it lives in. The idea it carries, that you write
  what a person gets before you build it, already landed in this repository as
  [#523](https://github.com/atlantic-blue/quay-crew/pull/523).
- [reviewer-gate.md](https://github.com/bmad-code-org/BMAD-METHOD/blob/main/src/bmm-skills/plan/bmad-architecture/references/reviewer-gate.md),
  MIT, read in full. It reviews an architecture document before handover, with parallel reviewers
  under different lenses. The shape is right for `plan-critic` and one line is worth taking: an
  inline self check does not count, because a fresh reader finds the divergence the author talks
  past. The rest names files and scripts that exist only inside that framework.
- [contains-studio/agents](https://github.com/contains-studio/agents). **No licence file.** I checked
  the repository and the licence field is empty. Nothing here is a candidate, whatever it says,
  because we may not copy it.

## What I did not verify

- I read the definitions. I ran none of them. No claim here about how a role behaves comes from
  watching it work.
- I listed all 158 names in the VoltAgent collection and read one of them. My statement that the
  collection is noun phrase lists comes from that one file and from the names. Treat it as a sample.
- I read two of the 137 agent definitions in wshobson/agents, chosen by name from the full list.
  Something useful may sit under a name I did not open.
- I did not read CrewAI, AutoGen, MetaGPT, OpenHands, Roo Code, SuperClaude or Agent OS. I checked
  their licences only. They define agents inside a running framework rather than as briefs a person
  writes, so a brief is not the thing they publish. That reasoning is an inference and not a reading.
- The BMAD trademark notice: I read it and it names the marks and not the text. I am not a lawyer and
  this is my reading of the file.
- `quay role list` needs a control plane, and this session had none. I read the fifteen roles from
  [`roles/`](../roles) in the repository at commit f48a48a instead.
