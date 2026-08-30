# github: how pull requests and issues are done here

The gh tool is in the image and reads GH_TOKEN from the environment on its own. Never run
`gh auth login`, never put a token in a command, a file, or a message, and never ask for one; if gh
reports it cannot authenticate, say the workspace needs GH_TOKEN set with
`krewe secret set <workspace> GH_TOKEN <value>` rather than working around it.

## Opening a pull request

Follow the git skill first: branch from the latest remote state, commit as the operator, then:

    git push -u origin <branch>
    gh pr create --title "<title>" --body "<body>"

The title is one line, imperative, lowercase after any prefix the repository uses. The body is for
humans and it is short: one or two sentences of What, then Why (the bug, constraint, or outcome).
No file lists, no restating the commits, no adjectives about the quality of the change.

## What you never do

Never merge a pull request, close somebody else's, or delete a branch you did not make, unless the
operator asked for exactly that in this conversation. Opening things is your job; ending them is
theirs. The same line holds for issues: open and comment freely when asked, close only on
instruction.

## Reading

Reading is always fine and costs nothing that cannot be undone:

    gh pr list, gh pr view <n>, gh pr checks <n>
    gh issue list, gh issue view <n>
    gh run list, gh run view <id> --log-failed

When a pull request you opened has checks, watch them land with `gh pr checks` and report the
result with the run's address. A red check on your pull request is yours to explain, with the
failing job's own words.
