**A project says where its work lands, so a session told to push has somewhere to push to.** A
project was a name. The repository its work goes to lived in a person's memory, so the address had to
be passed to every job by hand, and nothing could check that a job about to run had anywhere to push.
The acceptance run made a repository outside the crew, and the crew held no record that it was that
project's.

```
quay project repository atlantic-blue/transcript
quay project repository me/transcript atlantic-blue/transcript private
```

`quay project list` and `quay project create` say it back, and a project with none says so and says
what to type. A job declared in the project which names no repository of its own works in the
project's, so the session doing it is asked to push there and open a pull request against it. A job
that names its own keeps it: the project's is the default, not a ceiling.

The kind is a cost fact rather than a permission. A pipeline's minutes are free on a public
repository and metered on a private one, and that rule holds for every project this crew will ever
run while being said out loud once per project. Saying nothing records public, and the crew prints
what the kind costs beside the address, so the choice is read rather than remembered. A word that is
neither, `internal` for instance, is refused rather than taken for the default: recording a cost fact
nobody stated is worse than asking.

It is what the operator declared, not what the forge says. The crew does not go and look, so a
project can name a repository that does not exist yet, and one whose kind has since changed. Nothing
here creates a repository either. Fetching one is still a conversation, following the
[git skill](skills/git): this says which repository the project **is**, not how its files arrive.

The address rules are now one rule in [`internal/repository`](internal/repository), held by a job and
by a project alike, because two spellings of "this is an owner and a name" drift the day somebody
fixes one of them.
