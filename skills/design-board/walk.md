# design-board: the walk, call by call

Read `SKILL.md` first. It says what the design holds, the four checks before anything is written,
and where a status comes from. This file says what to call, and in what order.

## One milestone per design milestone

    gh api repos/<owner>/<repo>/milestones -f "title=M4. <title>" --jq .number

Keep the map from the design number to the GitHub number. A second run reads the milestone that
exists rather than making another one.

## One issue per slice

The title is `<id>: <name>`, so `S-36: A project holds features`.

The body carries these, in this order. It leaves out what the slice does not have.

- the intention
- the changes
- what it depends on
- what proves it
- any note
- the contract names
- how much of each contract this slice builds
- the files it touches

Close the body with a line naming the slice identifier and its milestone.

The block saying how much of each contract this slice builds comes from `partial_contracts`, one
line per entry. It never gets cut. It is the part that stops a session building a later slice's
work by accident.

Write the body to a file and pass `--body-file`. A body passed as a shell string loses its line
breaks. It also trips on every backtick in a file path.

Assign every issue to the operator. Attach every issue to its milestone.

Add the pull request link as the first line of a done slice's body, reading `**Shipped as** <url>`.

## Three calls put each issue on the board

Read the issue node identifier. Then call `addProjectV2ItemById`. Then call
`updateProjectV2ItemFieldValue` with the Status option. The add answers with the existing item when
the item sits on the board already, so it is safe to repeat.

## A board can close the issue for you

A new board carries a project workflow that closes an issue when its status becomes Done, and it is
on by default. Say so, so nobody reads a closed issue as a mistake.

## Running it again

The refresh is the same walk with three differences.

- A milestone that exists is read, not created.
- An issue whose title starts with the same slice identifier is edited, not created. Rewrite its
  body when the slice changed in the graph.
- Set the status again on a card that exists.

Close the issue of a slice the design removed. Never delete that issue, because a merged pull
request may name it.

## What it costs

Forty three slices took about four minutes: one call per milestone, one call per issue, three calls
per card. Run the issue creation and the board walk in the background, then read the log. Either one
runs past a two minute foreground limit.
