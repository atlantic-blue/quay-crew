**A hook holds prose to Simplified Technical English, for the part of it a program can measure.**
Every role here writes prose for a person: a pull request description, a changelog fragment, an issue
body, a commit message, a document. The standard for that prose is ASD-STE100. Before this, the
standard was a sentence in a brief, which is the position the merge rule was in before `merge-gate`.

`prose-gate` fires on `PreToolUse`. It reads a `.md`, `.markdown`, `.txt` or `.rst` file about to be
written. It reads the prose a command carries too: `--body` on `gh pr`, `gh issue` and `gh release`,
`-m` on `git commit`, and `--body-file`, where it opens the file. A file type decides what is prose,
so a Go file goes through.

It refuses five things. A sentence of more than 25 words. A paragraph of more than 6 sentences. The
present perfect and the past perfect. A continuous tense. A dash used as punctuation.

Each refusal names the file, the line, the sentence, and what to do to it. "Too long" is not a
refusal a writer acts on. "This sentence is 34 words, split it" is.

The standard holds fifty three rules and this gate holds five. A licence covers the approved
vocabulary and nobody publishes it as a list. No pattern finds an idiom. A noun cluster and a lone
"-ing" word both need a parser, and a parser guesses. So the gate refuses to guess at any of them,
and every refusal says which half a person still holds. Otherwise a session reads a passing write as
prose in the standard.

The gate is attached rather than seeded. `merge-gate` is seeded because it refuses one thing no
session is ever meant to do. This one refuses prose, and prose is what a role produces all day, so a
workspace opts in with `krewe hook attach <workspace> prose-gate`.

The number the issue asked for: the gate refuses 1296 of the 3795 paragraphs already in `docs/`,
`README.md`, `CHANGELOG.md`, `changelog.d/` and `roles/`. That is 34 per cent, and it is not a false
positive rate. Of 1514 refusals for length, 0 quote two sentences read as one. A sample of 20, read
by hand, held 20 real long sentences. The dash rule refuses nothing in `docs/` at all.

The prose here uses the house voice, which chooses long explanatory sentences, and most of it
predates the standard. So the thresholds stay at the standard's own numbers. A test in the hook's own
module runs that measurement on every run. A second test holds the gate's README to its own gate.
