# git: how work is done in a repository here

You clone what you work on. Nothing is cloned for you, so the first step of any work in a repository
is to put it there yourself. It goes in the workspace's volume, not in your own directory:
`/home/agent/shared` is shared by every session in this workspace, and `/home/agent/workspace` is
yours alone. One clone serves all of you, and each session works in a working tree of its own.

## Clone once, then take a working tree

Look for the repository before you clone it:

    ls /home/agent/shared/repos/<name>

Clone it there if it is not:

    git clone https://github.com/<owner>/<name>.git /home/agent/shared/repos/<name>

Then take a working tree of your own and work in it:

    git -C /home/agent/shared/repos/<name> fetch origin
    git -C /home/agent/shared/repos/<name> worktree add \
        /home/agent/shared/worktrees/$QC_SESSION_ID/<name> -b quay/$QC_SESSION_ID origin/HEAD
    ln -s /home/agent/shared/worktrees/$QC_SESSION_ID/<name> /home/agent/workspace/<name>
    cd /home/agent/workspace/<name>

QC_SESSION_ID is this session's own identifier, set for you. The tree has to sit under it: a clone
records where its working trees are, every session sees the same paths, and two sessions adding a
tree at one path take each other's away. The branch is your own for the same reason, because git
refuses to check out one branch in two trees.

Your tree is already there on a later task, so use it rather than adding it again. If
`/home/agent/shared` does not exist, this crew keeps no volume: clone into your working directory
instead and everything below is unchanged.

Authentication is already handled: the image carries a credential helper that reads GH_TOKEN from the
environment when git asks. Never put a token in a remote address, an argument, or a file, and never
ask for one; if a private clone fails, say the workspace needs GH_TOKEN set with
`quay secret set <workspace> GH_TOKEN <value>` rather than working around it.

## Branch first

Never commit to the default branch. Your tree is born on a branch of its own, so you are not on it.
Before changing anything, name a branch after the work and cut it from the latest remote state:

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
