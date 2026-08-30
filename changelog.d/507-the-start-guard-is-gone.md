**The start guard for the old data layout is gone, because the move it existed for is finished.**
`make up` ran a `home-check` target first. It refused to start when a crew's files were still in the
data directory under `~/.quaycrew` and `~/.quay/data` was not there yet. Starting in that state would
have mounted an empty directory, minted a new token, and looked exactly like a crew that had lost
every conversation. The refusal was worth having while any crew was still on the old layout.

None are. The condition can no longer be true, so the target refused nothing and only read as though
it did. That is the cost of leaving it in: somebody reading the Makefile sees a check on the path to
`up` and believes the start is guarded. A guard that cannot fire is worse than no guard, because it
buys confidence it cannot pay for.

Nothing replaces it. The target is out, with its comment, its step in `up`, `upgrade` and `install`,
and the two cases in [deploy/configuration_test.go](deploy/configuration_test.go) that drove it.

The tool keeps its own refusal, in [cmd/quay/home.go](cmd/quay/home.go). It says what to move and
where, and it covers more than the data directory. Somebody arriving with a crew from before the move
is answered there.
