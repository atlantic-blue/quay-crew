package main

import (
	"path"
	"strings"
)

// What counts as a test, and why the rule is a shape of a name rather than a list of files.
//
// A session is handed a repository, not a manifest, so nothing here can be told which files hold the
// tests. Every ecosystem marks them in the name instead, and it has to, because the runner finds them
// the same way: Go collects a file ending in _test.go, pytest collects test_ or _test, vitest and
// jest collect .test. and .spec., and a feature file is a scenario the runner reads. So the name is
// the fact, and this reads it.
//
// A directory counts for the same reason, and it is the half that matters most: `rm -rf features/`
// takes every scenario in the repository without touching a file whose name ends in _test.go.

// testDirectory is a directory whose contents are the tests, whatever the files inside it are called.
// A fixture under testdata is what a test asserts against, so a change to one changes the assertion.
var testDirectory = map[string]bool{
	"test": true, "tests": true, "spec": true, "specs": true,
	"__tests__": true, "testdata": true, "fixtures": true, "features": true,
}

// testSuffix is the end of a file name that says the file is a test.
var testSuffix = []string{
	"_test.go", "_test.py", "_test.rb", "_test.exs", "_test.ts", "_test.js",
	"_spec.rb", "_spec.ts", "_spec.js", "_test.java", "_test.cs", "_test.rs",
	".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".test.mjs",
	".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx", ".spec.mjs",
	"test.java", "tests.cs", "test.php", ".feature",
}

// testPrefix is the start of a file name that says the file is a test.
var testPrefix = []string{"test_", "spec_"}

// APath is a path a program was handed, so a bare word is a path: `rm features` names the directory.
// A Named word merely appears somewhere on a command line, so it has to look like a path before the
// directory rule reads it, or `go test ./...` and `make features` are refused for holding a word.
func APath(where string) (string, bool)  { return aTest(where, true) }
func ANamed(where string) (string, bool) { return aTest(where, false) }

// aTest says whether this path is a test, and names why it is read as one.
//
// The reason travels with the answer because it is half of the refusal. A session told only that a
// file is a test argues with the verdict; a session told the name ends in _test.go knows the rule and
// knows which of its files the rule covers.
func aTest(where string, bare bool) (string, bool) {
	where = strings.TrimSpace(where)
	if where == "" {
		return "", false
	}
	cleaned := path.Clean(strings.ReplaceAll(where, `\`, "/"))
	name := strings.ToLower(path.Base(cleaned))
	for _, suffix := range testSuffix {
		if strings.HasSuffix(name, suffix) {
			return "its name ends in " + suffix, true
		}
	}
	for _, prefix := range testPrefix {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return "its name begins with " + prefix, true
		}
	}
	// A word with no separator in it is only read as a directory where the program it was handed to
	// takes paths. Everywhere else it is as likely to be a make target or a subcommand.
	if !bare && !strings.Contains(where, "/") {
		return "", false
	}
	for _, part := range strings.Split(cleaned, "/") {
		if testDirectory[strings.ToLower(part)] {
			return "it is a " + part + " directory, or under one", true
		}
	}
	return "", false
}

// ATree says whether this path is the working directory itself, which is what a command that names no
// path at all is pointed at. Everything else is a named path, and whether that path holds a test is a
// question about the disk rather than about the name.
func ATree(where string) bool {
	where = strings.TrimSpace(where)
	return where == "" || where == "." || where == ".." || where == "./" || where == "*"
}
