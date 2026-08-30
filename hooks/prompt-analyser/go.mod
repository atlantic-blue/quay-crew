// The hook is its own module on purpose. It is a plugin: a thing somebody reviews, versions and
// hands to another system, so it does not share the system's dependencies and cannot import its
// internals. The standard library is all it needs.
module github.com/atlantic-blue/krewe/hooks/prompt-analyser

go 1.25.0
