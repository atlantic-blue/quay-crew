**The reserved words refuse however they are typed, and the refusal no longer advises typing one of
them.** `krewe workspace create System` answered "workspace name \"System\" cannot be part of an
address: use lowercase letters, digits and hyphens, for example \"system\"". Follow that advice and
the next answer is that a workspace cannot be called `system`, because it is the word for the level
above every workspace. The advice could not be followed, which reads as the rule not applying here.
`Crew` did the same thing one step further on: it suggested `crew`, which is also refused.

A name is lowercase letters, digits and hyphens, so neither word can carry a capital and still be the
name of anything. Whoever typed one meant the word. So the two reserved names are now read before the
general rule about names, and read whatever case they carry.

The same for the word where an address goes. `krewe secret list Crew` answered "this system has no
workspace \"Crew\"", which sends the operator looking for a workspace. It now says the level is
called `system` and to type that, exactly as the lowercase word already did. One spelling refused and
the next one waved through is the same quiet failure as the word being dropped.

**And the refusal is now read off the protocol rather than off a list.** The control plane refuses a
scope of the retired word on every call that takes a scope, and the test for it named those calls by
hand: eight of them, which was every one on the day it was written. A ninth call that carried a scope
and forgot the refusal would have left the suite green. The test now walks the service descriptor,
finds every call whose request carries a scope, and sends each one the retired word over a real
connection, so a call added later cannot be the one nobody remembered. It fails on finding no such
call, because a walk that runs nothing reports success.

The Go module path, the compose project, the sandbox image name and the repository still carry the
old word. Those are [#517](https://github.com/atlantic-blue/quay-crew/issues/517), which subsumed
them.
