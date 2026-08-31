# Where a session's context goes

Context is the budget that decides how good the work is. The system said how full a window was and
stopped there, so a session at eighty per cent could have filled up on the code it had to read, on
tool output it read once, or on its own repeated attempts, and nobody could say which.

This document holds the first measurement. Read it before proposing any change to how a session reads
code, and cite it in the proposal.

## What is counted

Every conversation is read back from the transcript the model runtime keeps, and every character of
it goes into one of four categories.

`reads` is the contents of files, however the session opened them. A reading tool counts, and so does
a reading command in the shell, matched on the first word: `cat`, `head`, `tail`, `less`, `more`, and
`sed` with `-n`.

`tools` is what every other tool returned: a search, a page fetched, the answer of a sub agent, and
every shell command that was not printing a file.

`turns` is the session's own words: what it wrote, what it thought, and the calls it made.

`told` is what reached the session from outside a tool: the task it was given, and the answers to its
questions.

The count is characters, not tokens. The transcript holds text and every model counts tokens its own
way, so a token count worked out from the text would be a made up number sitting beside the real one
the model reports.

A sub agent's records are left out. A sub agent fills a window of its own, and what it hands back
arrives in this conversation as tool output, where it is counted once.

## The run

Measured on 31 August 2026 over the conversation store of this crew's own sandbox: 192 conversations
and 42,839,991 characters. Reproduce it with the harness that produced it:

    KREWE_MEASURE_TRANSCRIPTS=~/.claude/projects/-home-agent-workspace \
      go test ./internal/sandbox/ -run TestTheContextSpendMeasurement -v -count=1

The harness needs a directory of transcripts and nothing else. It skips where nothing names one, and
it fails on a directory that holds none, so a run that measured nothing cannot report success.

The store grows every time somebody works, so a later run over the same directory reads larger than
the figures below. The shares are what to compare, not the totals.

## What it says

    reads      13,125,023   31%   dominates 36 conversations
    tools      13,945,996   33%   dominates 42 conversations
    turns      13,712,427   32%   dominates 24 conversations
    told        2,056,545    5%   dominates 90 conversations

**No category dominates.** Reads, tools and turns are within two points of each other. That is the
result, and it is the one that decides what to build next.

`told` dominates 90 of the 192 conversations and holds 5 per cent of the characters, because most
conversations are short: 104 of them are under a hundred thousand characters and together they hold
1.4 per cent of everything measured. The characters are where the budget is, so the three way split is
the answer and the count of conversations is not.

## What this says about reading code

Published work on agent harnesses claims that reading a symbol costs a fraction of reading the file it
lives in. Take that claim at its strongest, that every file read could be made free, and it removes 31
per cent of what fills a session's context. It leaves the other 69 per cent where it is.

So a change to how sessions read code is worth about a third of the budget at best, and the same
effort spent on what tools return, or on how many turns a session takes to get somewhere, is worth as
much. Nothing here says which of the three is easiest to move. It says none of them can be ignored.

Without the shell rule the answer looked completely different, and that is worth recording. Counting
only what a reading tool returned put reads at 2 per cent and tool output at 61 per cent of the same
corpus. The sessions in this crew are told to work through the shell, so they read their files with
`cat` and `sed -n`: 94 per cent of the tool output was the shell, and 47 per cent of that came back
from a command whose first word reads a file. Take `ReadsAFile` out of `contextspend.Of` and the run
above reproduces the 2 per cent. A breakdown that had shipped that way would have said reading code
was almost none of the budget, and nobody would have questioned it.

## Characters a token

The accounting counts characters and the model counts tokens, so the two are put in the same units to
be compared.

The usual figure is four characters a token. That is prose. Measured here it is **1.883**: 42,529,463
characters against 22,587,622 tokens, over the 101 conversations that hold two answers or more. The
code rounds it to 1.9.

The method needs no assumption. Between one answer and the next, the transcript grew by so many
characters and the model's own count of what it carries grew by so many tokens. Only the growth is
measured, because what the first answer carried is the system prompt and the tool definitions, which
the transcript does not hold.

A session's conversation is code, paths, identifiers, JSON and terminal output, and every one of those
costs more tokens per character than prose. Four would have reported every breakdown as covering half
of what it covers.

This describes this crew's traffic, not the model. Different work reads differently, so measure it
again rather than inheriting it.

## Holding the total against the model's own count

A breakdown whose parts do not add up to the model's total is a number that will be trusted and is
wrong. The four parts add up to the total by construction, which is arithmetic and proves nothing on
its own, so the total is held against what the model says it carries.

Over the whole run the breakdown accounts for 66 per cent of it. Over the 88 conversations above a
hundred thousand characters, which hold 42,239,098 of the 42,839,991, it is 80 per cent, and on the
ten largest it runs from 72 to 107 per cent.

The gap has two causes and both are expected.

The part no transcript holds is the system prompt and the definitions of every tool the session
carries. It is a fixed floor per conversation, so it is nearly the whole window on a short one and a
small part of a long one, which is why the share rises with the size of the conversation.

Over a hundred per cent means the transcript holds more than the model still carries, which is a
conversation the runtime compacted. It is printed rather than capped, because it says something true
about that conversation.

## What this does not measure

- A file read hidden behind a `cd`, an `awk` or a script is counted as tool output. The reads figure
  is a floor.
- Compaction is invisible from the transcript, so the breakdown covers the whole conversation rather
  than only what the window still holds.
- What the runtime injects around the messages is in the model's count and in no record here.
- Nothing is measured about whether a read was worth making. This says where the context went, never
  whether it should have gone there.
