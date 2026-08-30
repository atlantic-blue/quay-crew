# deploy-identity: prove the identity that applies your infrastructure may create it

Validation never talks to the account. `terraform validate` and a format check pass whatever the
deploy identity is allowed to do, so a wall of green checks says nothing about whether the apply can
run. The first command that talks to the account is the first place the truth arrives, and by then
the change is merged.

So before you open a pull request that creates infrastructure, prove the apply can happen.

## Four steps

1. **Name the identity that will apply it.** It is the role or the user the pipeline assumes, not
   the one your session holds. The project records it: `krewe target <workspace>/<project>` prints
   the account, the region and the identity. If nothing records it, ask. Do not guess.
2. **List every action the plan needs.** Read your own configuration, resource by resource. A bucket
   needs `s3:CreateBucket`, a table needs `dynamodb:CreateTable`, a function needs
   `lambda:CreateFunction` and `iam:PassRole`. The state backend needs its own reads and writes
   before any of that.
3. **Ask whether that identity is allowed each one.** `iam:SimulatePrincipalPolicy` answers the
   question and creates nothing:

       aws iam simulate-principal-policy \
         --policy-source-arn arn:aws:iam::123456789012:role/deploy \
         --action-names s3:CreateBucket dynamodb:CreateTable lambda:CreateFunction iam:PassRole \
         --query 'EvaluationResults[].[EvalActionName,EvalDecision]' --output table

   Name the resources with `--resource-arns` where you know them, because a policy scoped to two
   buckets answers `allowed` for the action and denies the bucket you are about to create.
4. **Read the answer.** `allowed` is a pass. `implicitDeny` means nothing grants it, and
   `explicitDeny` means something refuses it, which no added policy will fix. Both are a no.

## Say the result in the pull request

Name the identity, list every action you checked, and list every action that came back denied. A
reader who cannot see what was checked has to assume nothing was.

**A denied action stops you reporting the work as ready.** Say which identity, which action and
which resource, and what has to be granted. That report is the deliverable. It is worth more than
the pull request it holds up.

## Two traps

**A plan needs fewer permissions than an apply, so a green plan is not evidence.** A plan reads and
an apply creates. An identity with read only access plans the whole stack and cannot make one
resource of it.

**The first denial hides the rest.** An apply stops at the first refusal, so granting that one
action buys you the next refusal, one deploy each. Ask about every action in one call, so one
report names the whole gap.

## When the check cannot run

No credentials in the session, a credential that may not call the simulator, which is its own
`iam:SimulatePrincipalPolicy` permission, or a cloud with no simulator at all: say the check did not
run, and why, in the same place you would have put the result. Not run is not the same as passed.
That sentence is what tells whoever merges to look first.
