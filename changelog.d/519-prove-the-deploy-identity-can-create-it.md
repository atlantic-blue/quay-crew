**A job that writes infrastructure is now asked to prove the deploy identity can create it.** On the
acceptance run a job wrote Terraform for six resources, opened a pull request, and every check went
green in eleven seconds. The checks were `terraform validate` and a format check, and neither talks
to the cloud account. The pull request merged and the deploy died on the first command that did:
`AccessDenied` on `s3:CreateBucket`, because the identity that runs it holds read only access plus
two writes. It could not have created any of the six. Granting the action it named would have moved
the failure to the next resource, one deploy each.

Amazon answers that question without creating anything, through `iam:SimulatePrincipalPolicy`.
Nothing prompted the job to ask it.

[skills/deploy-identity/](skills/deploy-identity/) is the rule: name the identity that will apply the
change, list every action the plan needs, ask the simulator about all of them in one call, and put
the identity, the actions checked and any denial in the pull request. A denied action stops the job
reporting the work as ready. Where the check cannot run at all, for want of a credential or a
simulator, the brief says to write that down instead, because not run is not the same as passed.

It carries the two traps that each cost a cycle. A plan needs fewer permissions than an apply, so a
green plan is not evidence. And the first denial hides the rest, which is what turns one gap into one
deploy per missing action.

A fresh system gives it to every session rather than offering it, which is the third skill to be
seeded that way after git and github. A rule that waits to be attached is missing in every system
nobody set up, and that is where this failure happens. It names no secret, so no workspace loses it
for want of a credential.

It says nothing about what the pipeline does after the merge. A release is done when the deployed
address answers, and reading what came back is [#450](https://github.com/atlantic-blue/quay-crew/issues/450).
