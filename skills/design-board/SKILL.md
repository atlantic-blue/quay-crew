# design-board: how a design reaches a project board

A project keeps its design in `.greenlight/`. This skill copies that design onto a GitHub project
board. A person who does not read the repository then sees what the work is and where it reached.
It writes to GitHub only. It never writes to the design.

## What it reads

`GRAPH.json` holds the slices. Each slice carries `id`, `milestone`, `name`, `intention`,
`depends_on`, `contracts`, `partial_contracts`, `touches`, `changes` and `proves`. Some slices also
carry a `note`, a `gate` or a `why` line.

`ROADMAP.md` holds the milestone titles, one per line, in this form:

    **Milestone 4. A design carries a numbered path, and the operator reads it.**

Read both files from the remote, never from a checkout, because a checkout goes stale:

    gh api repos/<owner>/<repo>/contents/<path> --jq '.content'

That answers base64, so decode it.

## Four checks before anything is written

**The token carries the project scope.** Run `gh auth status` and read the scope list. Without the
project scope the board writes fail after the issues exist.

**The board's own fields.** Read the Status field identifier and its option identifiers from the
board you write to. They differ on every board, so never carry them from another one. The query is
a `projectV2` read of `fields`, asking for `ProjectV2SingleSelectField` with its `options`.

**The items on the board.** Count them. A board with items needs the refresh path, not the create
path. Match a card to a slice by the identifier at the start of the issue title.

**The repository's milestones.** A repository usually carries milestones from earlier work. Name
the new ones `M<number>. <title>`. They then sort, and they cannot collide with the old ones.

## The order is milestones, then issues, then board cards

An issue cannot name a milestone that does not exist. A card cannot point at an issue that does not
exist.

## Where the status comes from

Never from a guess, and never from the graph. The graph says what the work is, not where it reached.

- **Done** is a slice whose pull request merged. Read `gh pr list --state merged`. Match each merge
  to the slice it built.
- **In Progress** is a slice a session holds now. Read `krewe sessions <workspace>/<project>` and
  the open pull requests. A slice with an open pull request is in progress, not done.
- **Todo** is everything else.

Say which slices you marked done, and what evidence made each one done. A wrong match is then
visible rather than silent.

## What it does not do

It never writes to the design. The design is the source and the board is the copy. When the two
disagree the graph wins, and the board is refreshed.

It sends no message and it comments on nothing. Making an issue is not a message. A comment on one
is a message, and that needs asking first.

`walk.md` in this directory says how to write each milestone, each issue and each card, what a
second run changes, and what the walk costs. Read it when you are about to run one.
