# git: how work is done in a repository here

You clone what you work on. Nothing is cloned for you, and your working directory starts empty, so
the first step of any work in a repository is to put it there yourself:

    git clone https://github.com/<owner>/<name>.git

Clone into your working directory. Authentication is already handled: the image carries a credential
helper that reads GH_TOKEN from the environment when git asks. Never put a token in a remote address,
an argument, or a file, and never ask for one; if a private clone fails, say the workspace needs
GH_TOKEN set with `quay secret set <workspace> GH_TOKEN <value>` rather than working around it.

## Branch first

Never commit to the default branch. Before changing anything, cut a branch from the latest remote
state and work there:

    git fetch origin
    git switch -c <branch> origin/HEAD

Name the branch after the work, lowercase, words joined with hyphens.

## Commit as the operator

Your author and committer identity come from the operator's own git configuration, which the
workspace mounts for you. Do not set user.name or user.email, and do not add any signature,
attribution line, or mention of a tool to a commit message. The commit is the operator's.

If a commit fails because git does not know who you are, say the workspace needs the operator's
configuration mounted with `quay secret mount <workspace> gitconfig ~/.gitconfig`, rather than
inventing a name and an email to get past it.

Whether you sign is already decided for you, so leave commit.gpgsign alone. A workspace that holds
a signing key signs; one that does not, does not.

Stage the specific files you changed, by name. Never `git add .` or `git add -A`: a sweep stages
whatever else happens to be lying around, and you may not be the only thing writing here.

Commit messages are one line: imperative, lowercase, no trailing period, saying what the change
does. One logical change per commit.

## Leave the history alone

Do not rewrite what has been shared: no force push, no amend or rebase of anything already pushed.
If a commit is wrong, follow it with a correcting commit.
