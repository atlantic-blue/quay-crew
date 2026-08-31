**A pull request that creates infrastructure is now refused until it says the deploy identity may
create it.** The rule shipped as [skills/deploy-identity/](skills/deploy-identity/), and a skill is a
rule a session reads. Nothing checked that it was read. The failure it exists for is a job that opened
a pull request over six resources with every check green, so a rule that only reaches the sessions
that were going to follow it anyway leaves the failure exactly where it was.

[hooks/deploy-identity-gate/](hooks/deploy-identity-gate/) is the check. It holds the two halves of
the rule a check can be exact about. A pull request whose change touches a `.tf` or a `.tf.json` file
is refused unless its body names the identity that will apply it and the actions that identity needs.
A pull request whose body reports an action that came back `implicitDeny` or `explicitDeny` is refused
outright: the identity cannot create what the change declares, so the report is the deliverable and it
is worth more than the pull request it holds up.

The refusal carries the four steps rather than sending the session away to find them: name the
identity with `krewe target`, list the actions, ask `iam:SimulatePrincipalPolicy` about all of them in
one call, and put the answer in the body. Where the check cannot run at all, saying so in the body is
a pass, because not run is not the same as passed and that sentence is what tells whoever merges to
look first.

It reads the command the way a shell would and the change the way git does, so it covers `gh pr
create` in every spelling and the same call made over `gh api`, `curl` and `wget`. Where it cannot
read the change, for want of git or of a default branch to compare against, the command goes through:
a gate that refuses what it cannot read refuses the work, and every role opens a pull request on every
slice. A page that explains what `implicitDeny` means is not a report of one, so the documentation
that teaches the rule is not refused by it.

A fresh system is under it, beside the merge gate, on the same argument: a gate an operator has to
remember to attach is off in every system nobody set up, and that is the system this failure happened
in. It declares no binary and names no secret, so no image and no workspace can lose it.
`krewe hook detach system deploy-identity-gate` is how somebody decides otherwise.

What it does not hold: infrastructure it cannot recognise by name, which is everything that is not
Terraform, and a denial written only in prose. Both are named in the hook's own page rather than left
for somebody to find.
