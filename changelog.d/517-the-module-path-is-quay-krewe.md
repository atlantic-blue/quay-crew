**The Go module path is `github.com/atlantic-blue/quay-krewe`, and the repository is renamed to
match.** The module path and the repository address have to be the same string, or the module proxy
cannot find the code. They were different: `go install github.com/atlantic-blue/krewe/cmd/krewe@latest`
answered `Repository not found`, because the path said `krewe` and the repository said `quay-crew`.

Four modules move, not one. The root, and the three hooks under `hooks/`, each of which is its own
module by design. Nothing outside `hooks/` imports them.

The protobuf option in `buf.gen.yaml` moves with it, so `buf generate` writes the new path into the
generated code. The descriptor carries the path with a length in front of it, so this is a
regeneration and not a search and replace.

**What an operator with a clone types, once:**

```
git remote set-url origin https://github.com/atlantic-blue/quay-krewe.git
```

GitHub redirects the old address, so a clone, a fetch and a push all keep working. The redirect ends
the day somebody creates a new repository at the old name. Every issue link, pull request link and commit link written
before the rename still resolves, which is why this change leaves them alone.

The example address in a refusal moves with the repository. A repository that is not an owner and a
name is refused with `atlantic-blue/quay-krewe` as the shape to copy. A claim on a piece of work is
written `atlantic-blue/quay-krewe#540`.

This is the last piece of the rename. `docs/RENAME.md` holds the plan, and issue 517 closes with it.
