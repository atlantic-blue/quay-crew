// The hook is its own module, the same way the merge gate and the analyser are. It is a plugin:
// something somebody reviews, versions and hands to another system, so it does not share the system's
// dependencies and cannot import its internals. The standard library is all it needs.
module github.com/atlantic-blue/krewe/hooks/deploy-identity-gate

go 1.25.0
