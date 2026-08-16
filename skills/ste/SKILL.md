# ste: writing that cannot be read two ways

Simplified Technical English is a controlled form of English, standardised as ASD-STE100 by the
AeroSpace and Defence Industries Association of Europe. It exists because a technician reading
"close the valve" can take "close" as an instruction or as a description of a valve that is nearby,
and on an aircraft that difference matters. It removes what lets a sentence be read two ways: a word
carrying more than one meaning, and a sentence with more than one possible structure.

The same discipline is worth having for a different reader: something that parses your English with
nobody there to resolve what you meant. An error message, a tool description, a message to another
session, a status line, a prompt.

## Do not write this way by default

Holding this skill is not a reason to use it. It applies only when the operator asks for it in the
conversation: for Simplified Technical English by name, for a rewrite in it, or for text another
machine has to parse.

**Never apply it to writing meant for a person to enjoy or be persuaded by.** This style is
deliberately flat, and flatness is the point: it strips rhythm, register and voice, which are what
that writing is made of. So it does not touch a blog post, a newsletter, a social post, marketing, a
pull request description written in somebody's own voice, or anything whose workspace or project
context describes how the writing should sound. Where this skill and a context disagree, the context
wins and this stays out of the way.

If you cannot tell which kind of text you are looking at, ask. Tasking a paragraph somebody wrote
carefully into something correct and dead is worse than leaving it alone.

## The rules

- **One word, one meaning.** One word per action, every time. If it is "check", it is never also
  "verify" or "confirm": three words read as three different actions.
- **One part of speech per word.** "Apply oil to the valve", not "oil the valve".
- **Active voice, and name who acts.** "The agent deletes the file", not "the file is deleted".
- **Simple tenses.** "We received the report", not "we have received the report".
- **One instruction per sentence.** "Open the file. Read line three."
- **Twenty words for an instruction, twenty five for a description.** Past that, split it.
- **Three words at most in a noun phrase.** "Fuel pump valve", never "high pressure fuel pump inlet
  valve assembly".
- **Keep the words that carry the grammar.** No dropped article, subject or verb to save room.
- **One topic per paragraph, six sentences at most**, and a numbered list for three or more steps.
- **Keep the technical words you need, and define each one once.**

Never drop a fact, a number, a condition, a scope or a safety qualifier to make a sentence shorter.
Where a rule can only be satisfied by losing precision, keep the precision and say why.

`rewriting.md` in this directory has the method, what to hand back, and worked examples. Read it when
you are actually doing a rewrite.

## What this is not

This applies the rules of the standard. It does not reproduce the approved dictionary of about nine
hundred words, which is ASD's document and theirs to distribute, so this is a tool for writing that
reads one way only rather than a certified authoring tool. For real maintenance documentation the
official standard at https://www.asd-ste100.org/ is the source of truth and every word is checked
against the real dictionary.

Simplified Technical English and ASD-STE100 are a copyright and a trademark of ASD, Brussels.
