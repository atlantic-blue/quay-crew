# prose-gate

Holds the prose this system writes to Simplified Technical English, for the part of it a program can
measure.

Every role here writes prose for a person. A pull request description, a changelog fragment, an issue
body, a commit message, a document. The standard for that prose is ASD-STE100, at
https://www.asd-ste100.org/. Before this hook, the standard was a sentence in a brief. That is the
position the merge rule was in before `merge-gate`.

A hook holds part of the standard. Not all of it. To say which part is the whole design.

## What it measures, and can refuse

Four rules. Each one is exact, and a program decides it without a judgement.

- **A sentence of more than 25 words.** The standard allows 20 words for an instruction and 25 for a
  description. To tell those two apart is a reading of what the sentence is for, and a hook must not
  make that reading. So the gate checks the wider number. The narrower one stays in the brief.
- **A paragraph of more than 6 sentences.**
- **The present perfect and the past perfect.** `has shipped`, `had run`. The standard allows the
  infinitive, the imperative, the simple present, the simple past and the simple future.
- **A continuous tense.** `is running`, `was reading`. This is the measurable half of the standard's
  rule about "-ing" words, and only that half.
- **A dash used as punctuation.** The em dash, the en dash, and the hyphen with a space beside it. A
  hyphen inside a word is structural, so `kebab-case` and a command flag both go through.

## What it does not measure, and does not guess at

- **The approved vocabulary.** The standard holds about 900 words, each with one meaning. A licence
  covers the dictionary and nobody publishes it as a list. A hook with its own list refuses prose in
  the name of somebody's guess at the standard.
- **Idiom and metaphor.** "Fishing in that pond" is six ordinary words. No pattern finds it.
- **A noun cluster of more than 3 nouns.** To find a noun cluster, a program must know which words
  are nouns. English adds nouns and verbs freely, so a program needs a parser, and a parser guesses.
- **An "-ing" word that stands alone.** A gerund, a participle and a technical noun all look the
  same. Only the form of "be" in front of one makes the answer certain.

Those stay in the brief, where a person reads them. Every refusal says so, so a session does not read
this gate as the whole standard.

## Where it fires

`PreToolUse`, on two matchers.

**`Write`, `Edit` and `MultiEdit`,** for prose files only. The file type decides, and nothing else:
`.md`, `.markdown`, `.txt` and `.rst`. A Go file is not prose. A gate that measured sentence length
in source refuses every file in this repository on its first firing.

**`Bash`,** for the prose a command carries as an argument. `--body` and `-b` on `gh pr`, `gh issue`
and `gh release`. `-m` and `--message` on `git commit` and `git tag`. `--body-file` and `-F` on the
same `gh` commands, where the gate reads the file. A rule that a document meets and a pull request
body does not is a rule with a way around it.

It reads the command the way a shell reads it, far enough to find the program and its flags. Nothing
here expands a variable, resolves a glob or runs anything.

## What it does not hold

- **A body in a heredoc.** `gh pr create --body-file - <<'EOF'` puts the prose after the command
  rather than in it. The reader stops at the redirection.
- **A body built by a substitution.** `--body "$(cat body.md)"` hands the gate the substitution.
- **`gh api`.** Its `-F` names a field rather than a file, so the gate leaves that command alone.
- **Prose from a tool that is not Bash, Write, Edit or MultiEdit.**
- **An example of bad prose, quoted inside a code span.** A code span counts as one word, whatever is
  inside it. That is why `is running` sits in backticks above. The same words in plain prose are
  refused, which is correct: this gate cannot tell a quotation from a claim.
- **Anything, if the operator takes the hook off.** `krewe hook detach <workspace> prose-gate`.

## How it refuses

Exit code 2, with the reason on standard error. The runtime hands that to the session.

Each refusal names the file, the line, the sentence and what to do to it. "Too long" is not a refusal
a writer acts on. "This sentence is 34 words, split it" is. One firing reports five refusals and says
how many it keeps back, because a writer with forty of them rewrites the document by guessing.

Everything else exits 0, including a payload the gate cannot read and a file it cannot open. The gate
fires on every write and every command a session makes. A gate that refuses what it cannot read
refuses the work, and a broken hook must not stop a system.

## Attached, not seeded

`merge-gate` is seeded, because it refuses one thing that no session is ever meant to do. This gate
is different. It refuses prose, prose is what a role produces all day, and the rules are a style
somebody chooses. So a workspace opts in with `krewe hook attach <workspace> prose-gate`.

## How much of this repository's prose it refuses

The issue that asked for this gate asked for one number. Run the gate across the prose already here
and say how many paragraphs it refuses. The issue named that number the false positive rate, on the
premise that the prose here is the standard the gate aims at.

The premise is false, and the measurement says so. `corpus_test.go` runs the gate over `docs/`,
`README.md`, `CHANGELOG.md`, `changelog.d/` and `roles/` on every test run, and prints the current
figures. On 31 August 2026, against `f9b8062`:

    64 documents, 3795 paragraphs, 1296 refused (34 per cent)
    length 1514, paragraph 68, tense 458, dash 96

Those refusals are correct. Three measurements say so.

- **The length rule refuses a real sentence every time.** A wrong refusal needs a sentence boundary
  the reader missed, which joins two sentences into one over the limit. A joined sentence keeps the
  full stop between its halves, so every one is findable rather than a matter of sampling. Of 1514
  refusals, 0 hold an inner full stop. `TestNoRefusalIsMadeOfTwoSentencesReadAsOne` gates on that
  number.
- **A sample of 20 length refusals, read by hand, held 20 real sentences of more than 25 words.**
- **The dash rule refuses nothing in `docs/` and nothing in `CHANGELOG.md`.** This repository's own
  house style already forbids a dash. All 96 refusals sit in `roles/`, in briefs imported from
  published agent roles, and every one is a real em dash.

The prose here is not Simplified Technical English and it never claimed to be. `docs/` and
`CHANGELOG.md` use the house voice, which chooses long explanatory sentences. The median sentence in
`CHANGELOG.md` runs to 18 words and the ninetieth percentile runs to 37. A rule that allows 25 then
refuses about a third of it, correctly.

So the thresholds stay at the standard's own numbers. An operator needs one thing from this section
before attaching the gate. A session that edits `docs/` here meets a refusal on about a third of the
paragraphs it writes, because those documents predate the standard.

`roles/` is the closest thing here to prose written as short instructions, and it refuses far less.
`roles/wrapper/ROLE.md` refuses 1 paragraph in 135. `roles/assessor/ROLE.md` refuses 3 in 157.

This README passes its own gate. `TestTheGatesOwnDocumentPassesTheGate` holds it there.

## Building it

    make hooks

The entry point is `bin/hook`, built rather than committed. One committed binary runs on one
processor type, and this image builds on both arm and amd machines. This is its own Go module and it
needs the standard library only, so `go test ./...` at the root does not reach it. `make test` runs
it by name.
