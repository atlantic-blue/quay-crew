# Doing a rewrite

Read `SKILL.md` first, and in particular the part that says this is off unless the operator asked
for it.

## The method

Read the whole thing once before changing anything, so you know what it still has to say afterwards.

Then go sentence by sentence. For each one, name the rule it breaks before you touch it, and rewrite
to fix that rule and nothing else. A rewrite that also improves the phrasing is a rewrite nobody can
check.

If the text already reads one way only, say so and change nothing. Forcing edits onto compliant
writing is the most common way this does harm.

## What to hand back

The rewritten text first, because that is what was asked for.

Then one line per change: the rule, the original wording, the new wording. Somebody has to be able to
disagree with a single change without unpicking the whole thing.

Then anything you deliberately left alone, and why. That is almost always because the only way to
satisfy the rule was to lose a fact, a number, a condition, a scope or a safety qualifier, and the
precision is worth more than the rule.

## Worked examples

**Present perfect becomes simple past.**
Before: "We have received your request and it is being processed."
After: "We received your request. The system processes it now."
Two rules: the perfect tense, and two statements in one sentence.

**A noun stack becomes a phrase.**
Before: "the agent task queue priority handler"
After: "the handler that sets task queue priority"
Four words stacked is three more than a reader can hold.

**A dropped subject comes back.**
Before: "Files not backed up will be lost."
After: "The system deletes any file that was not backed up."
The original leaves which files ambiguous, and leaves the actor out entirely.

**One instruction per sentence.**
Before: "Open the file and read line three, then check whether it matches the schema."
After: "Open the file. Read line three. Compare line three against the schema."
Three instructions, and in the original the last one is the easiest to lose.

**Precision beats the rule.**
Before: "Do not restart the service while a migration is running, because a partial migration leaves
the table in a state the next migration cannot read."
After: unchanged, and say so. It is thirty one words and breaks the length rule. Splitting it puts
the instruction and the reason in different sentences, and the reason is what stops somebody deciding
the instruction does not apply to them.

## Where the real standard sits

These are the rule categories of the standard, applied by hand. The specification carries fifty three
writing rules across nine sections, plus a dictionary of about nine hundred approved words, each with
one meaning and one part of speech, and about twelve hundred words to avoid with replacements.

That dictionary is not reproduced here. When exact approved wording matters, download the standard
from https://www.asd-ste100.org/ and check word by word against it.
