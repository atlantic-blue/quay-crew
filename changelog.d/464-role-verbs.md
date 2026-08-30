**A role says `verbs`, which is the word kubernetes uses.** A role manifest declared what a session
running as it may call under `may`, and both halves of that file were the crew's own invention. The
console is shaped like k9s, k9s is shaped like kubectl, and kubectl has one settled word for the
question: a rule is api groups, resources and verbs, and `kubectl auth can-i create jobs` is how it
is asked. Borrowing it costs nothing and every operator arrives already knowing it.

The word travels the whole way rather than stopping at the file: the manifest key, the field on the
wire, the column in Postgres and the line `quay role show` prints. A role imported before the rename
keeps the verbs it declared, and the migration renames the column rather than adding a second one.

A manifest still saying `may` is refused at import, and the refusal names `verbs`. Left to yaml, the
refusal reads `field may not found in type role.manifest`, which names a Go type rather than the word
to write. Ignored instead of refused, the role would grant nothing and read exactly like one that
holds, which is the failure [#459](https://github.com/atlantic-blue/quay-crew/pull/459) had for a
different reason.

Two decisions from [#464](https://github.com/atlantic-blue/quay-crew/issues/464) are not taken here.
`job.read` is still one verb where kubernetes has `get` and `list`, and splitting it is a permission
change rather than a rename. The manifest still answers both questions in one file, where kubernetes
splits the role from the pod spec: a crew has no role binding and no identity sharing a role, so one
file a person writes and hands over whole is the better trade.
