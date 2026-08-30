**The level above every workspace is called `system`.** It was called `crew`, which was the word for
the product as well as the word for the level, so `quay secret set crew TOKEN` read as a sentence
about the whole product rather than as an address. The level is now `system` everywhere a person
meets it: `quay secret set system`, `quay skill attach system`, `quay context set system`,
`quay job list system`, the console, the listings and the manual.

`crew` is refused by name rather than dropped. Typed where an address goes it says the level is
called `system` and to type that, and the control plane refuses a scope of `crew` the same way, so a
tool from an older build is told what changed rather than being answered with a workspace that does
not exist. A workspace still cannot be called `crew`, for the same reason it cannot be called
`system`: one holding either word would quietly take what was meant for every workspace.

Nothing an operator has moves. `system_secrets`, `system_skills`, `system_hooks` and `system_roles`
are the four tables renamed in place, and the context held at that level has its scope updated in the
same migration, so the credentials, the skills and the context document a system already holds are
the same rows afterwards. `QC_CREW_RESERVE_MEMORY` and `QC_CREW_RESERVE_PROCESSOR` are still read
under those names, and the control plane says they have moved rather than falling back to the default
floor in silence.

The Go module path, the compose project, the sandbox image name and the repository keep the old word
for now.
