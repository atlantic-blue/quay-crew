# deploy-identity-gate

Refuses a pull request that creates infrastructure until it says the identity that will apply it may
create it.

A job wrote Terraform for six resources and opened a pull request. Every check went green in eleven
seconds. The checks were a format check and a validate, and neither one talks to the cloud account.
The pull request merged, and the deploy died on the first command that did: `AccessDenied` on
`s3:CreateBucket`, because the identity that runs it holds read only access. It could not have created
any of the six.

The rule against that is [`skills/deploy-identity`](../../skills/deploy-identity/SKILL.md), and a
skill is a rule a session reads. This is the check. It reads the command the session is about to run,
and it holds the two halves of the rule that a person can be exact about.

## What it refuses

It fires on `PreToolUse` for `Bash`.

**A pull request that creates infrastructure and carries no report.** The change touches a `.tf` or a
`.tf.json` file, and the body does not name the identity and the actions that identity needs. The
refusal names the files it read, and it carries the four steps rather than sending the session away to
find them.

**A pull request whose body reports an action that came back denied.** A line naming an action and
either `implicitDeny` or `explicitDeny`. The session asked, it was told no, and it is opening the pull
request anyway. There is no way through this one that opens a pull request: the report is the
deliverable, and it is worth more than the pull request it holds up.

Both halves cover `gh pr create` however it is spelled, including under `sudo`, inside `bash -c`,
after another command, and with a repository named first. They also cover the same call made over the
interface underneath it: a write to `repos/<owner>/<repo>/pulls` with `gh api`, `curl` or `wget`. A
gate that knows one spelling is a gate the next spelling walks through.

A body given as `--body-file` is read off disk, because that is the form a long body actually takes.
A `--fill` on a change that creates infrastructure is refused: the body comes from the commit messages
and there is nothing on the line that could carry the report.

## What counts as a report

The identity and at least one action, in the body. An identity with no actions says nothing was asked,
and actions with no identity say nobody was asked about them.

Or the honest third answer, in the words `did not run`, `could not run` or `cannot run`. No credential
in the session, a credential that may not call the simulator, or a cloud with no simulator at all.
That is a pass here and it is a pass nowhere else. It puts the sentence in front of whoever merges,
which is the whole of what it buys.

## What it does not refuse

**Infrastructure it cannot recognise by name.** Terraform is what it reads. CloudFormation, the Cloud
Development Kit, Pulumi and a serverless manifest all declare resources under names that are also
ordinary names for ordinary files, and a gate that guessed at those would refuse work it was never
asked to guard. The skill still covers them. This does not.

**Anything, when it cannot read the change.** No git in the image, a directory that is not a
repository, or no default branch to compare against: each one answers with no files, and the command
goes through. The denial half still holds, because it reads only the body.

**A denial written in prose.** It reads the two words the simulator answers with. "The role is not
allowed to make the bucket" is a denial and this gate will not see it.

**A page that explains what the decisions mean.** A line has to name an action as well as a decision,
so documentation about `implicitDeny` is not a report of one. This file is one of those pages.

**Anything, if the operator takes the hook off.** `krewe hook detach system deploy-identity-gate` is
how somebody decides otherwise.

## How it refuses

Exit code 2, with the reason on standard error, which the runtime hands to the session. Everything
else exits 0, including a payload it cannot read. It fires on every Bash command every session runs,
so a gate that refuses what it does not understand refuses the work, and a broken hook must not be
able to stop a system.

It asks whether the command opens a pull request before it reads anything else, so a session that runs
a hundred commands pays for git on the one command this gate is about.

## Reading a command line

`command.go` is the merge gate's reader, carried across rather than shared. A hook is its own module
by design, so it cannot import another hook, and one copy each is the price of that. The alternative
is a library both depend on, which makes a hook part of the system again.
