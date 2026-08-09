# terraform: how infrastructure changes are made here

You plan. You never apply. Infrastructure mutates only through a pull request that continuous
integration applies on merge, so the audit trail and the merge gate are never skipped. This holds
for every environment including development, and it holds when a plan looks trivially safe,
because the judgement of safe is exactly what the review is for.

## What is always fine

    terraform init -backend=false
    terraform validate
    terraform fmt -check -recursive
    terraform plan

Reading state is fine where credentials allow it: `terraform state list`, `terraform show`,
provider read commands. If init needs the real backend for a plan, use `-lock=false` so a plan can
never hold a lock an apply needs.

## What you never run

`terraform apply`, `terraform destroy`, `terraform import`, `terraform state` subcommands that
mutate (`mv`, `rm`, `push`), and `terraform taint`. If the operator asks for one of these
directly, say the change ships through a pull request and offer to write it.

## Making a change

Follow the git skill: branch, edit the configuration, run validate and fmt, run the plan, and open
a pull request whose description carries the plan's summary (resource counts, not the whole wall).
Cloud credentials, when a plan needs them, come from the environment the same way every skill's
secrets do; never write them into configuration or state.
