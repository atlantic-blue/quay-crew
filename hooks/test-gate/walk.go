package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Whether a directory a command takes whole has any test under it.
//
// The name of a directory says nothing about what is in it. `rm -rf build/` is ordinary work and
// `rm -rf internal/` takes every test in there with it, and the two read the same to any rule about
// names. So this reads the disk, which the hook can do: it runs inside the sandbox, in the session's
// own working directory, with the repository in front of it.

// entries bounds the walk. A repository this system works in is thousands of files, and a hook that
// walked an unbounded tree would hold the session's tool call open until the runtime's timeout.
const entries = 20000

// skipped are the directories a walk does not go into. What is inside them is not this repository's
// tests, and they are the largest directories on any machine that has them.
var skipped = map[string]bool{".git": true, "node_modules": true, "vendor": true, "target": true}

// HoldsATest says whether this path is a directory with a test somewhere under it.
//
// A path that is not a directory answers no, because the name rules already read a file. A path that
// does not exist answers no, because a command cannot take away what is not there. A walk that runs
// out of entries answers yes: a directory too big to read is a directory this gate cannot clear, and
// the safe answer for a boundary is the one that refuses.
func HoldsATest(where string) bool {
	info, err := os.Stat(where)
	if err != nil || !info.IsDir() {
		return false
	}
	if why, is := APath(where); is {
		_ = why
		return true
	}
	seen, full := 0, false
	_ = filepath.WalkDir(where, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && skipped[strings.ToLower(entry.Name())] {
			return filepath.SkipDir
		}
		if seen++; seen > entries {
			full = true
			return filepath.SkipAll
		}
		if _, is := APath(path); is {
			full = true
			return filepath.SkipAll
		}
		return nil
	})
	return full
}
