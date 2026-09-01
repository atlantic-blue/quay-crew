**A system keeps everything it owns in `~/.krewe`, and `KREWE_HOME` names it.** The directory was
`~/.quay`, under the name the product had before this one. The command a person types is `krewe`, so
the directory beside it says krewe.

**The move is one command, and the tool refuses to start until it is done.** The directory holds what
nothing can make again: the system token, the driver token, the sealing key that unseals every
secret, and every conversation. A build that read the new path and started would come up on a
fresh token, with no sealing key. Every conversation would read as lost. So the tool stops before it
reads a token or an address, and prints the move:

```
mv ~/.quay ~/.krewe
```

There is no `mkdir` in front of it, and that is deliberate. `mkdir -p ~/.krewe` followed by that `mv`
puts the whole directory inside the new one, a level below anything that looks for it. The operator
is then left with a system that still cannot find its own token.

**The guard reads what the directory holds, not whether it exists.** `make config` makes the
directory and writes the configuration file into it before anything starts. A check on existence alone
would pass on that empty directory while every conversation sat in the old one. That is the exact
state this refusal exists for.

**`QUAY_HOME` still names the directory, for one release.** It is in shell profiles, in scripts and in
service files. A build that stopped reading it would send an operator who exports it to a fresh
directory, which is the same loss by another road. `KREWE_HOME` wins where both are set, so an
operator can move off the old one. The makefile reads both the same way.

The release that stops reading `QUAY_HOME` is the one that stops reading the retired container name.

Two names inside a container do not move. A sandbox that is up wrote `/tmp/.quay-setup-<skill>` to
say its skill was set up. The tmux session an open conversation lives in is called `krewe` already. A
build that renamed the first would run every skill's setup a second time, in a container that already
ran it.
