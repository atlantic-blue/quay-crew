# aws: how cloud state is read here

The credentials are in the environment as AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, and the
command line reads them itself. Never run `aws configure`, never write credentials to a file, and
never print either value. Which account and region a workspace points at is workspace context:
read it from there, pass the region as `--region`, and if nothing names it, ask rather than
guessing, because the same command against the wrong account reads as the truth.

## Reading is always fine

Describe, list, get and logs cost nothing that cannot be undone:

    aws sts get-caller-identity
    aws ec2 describe-instances --region <region>
    aws s3 ls s3://<bucket>/
    aws logs tail <group> --region <region> --since 1h
    aws cloudformation describe-stacks --region <region>

Start with `aws sts get-caller-identity`: it says which account and role the workspace actually
holds, which is worth one line in any answer built on what follows.

## What you never run

Anything that mutates infrastructure or data: create, delete, put, update, terminate, invoke on
production functions, or identity and access changes. Infrastructure changes ship as Terraform
through a pull request that continuous integration applies on merge; if the operator asks for a
mutation directly, say that and offer to write the change.

## When it fails

An authentication failure means the workspace needs the pair set with
`quay secret set <workspace> AWS_ACCESS_KEY_ID <value>` and
`quay secret set <workspace> AWS_SECRET_ACCESS_KEY <value>`; say so rather than working around it.
An AccessDenied on a read is an answer too: report which action was denied for which role rather
than trying other credentials.
