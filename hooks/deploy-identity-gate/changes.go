package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// This file is the one place the gate looks outside its own arguments. It reads the change the
// session is about to open a pull request for.
//
// Everything here is best effort, and every failure answers with no files, which the gate reads as
// nothing to guard. A gate that refuses what it cannot read refuses the work, and this hook fires on
// a command every role runs on every slice. A sandbox without git, a directory that is not a
// repository, a repository with no default branch to compare against: each one lets the command
// through.

// reading bounds how long git may take. The hook's own timeout is longer, and a session waiting on a
// gate that hung is a session that stopped.
const reading = 3 * time.Second

// theDefaultBranch is where a pull request merges into, in the order worth trying. The first name
// that resolves wins.
var theDefaultBranch = []string{"origin/HEAD", "origin/main", "origin/master", "main", "master"}

// Changes is the files this branch changed against the branch it will merge into, and nothing at all
// when git cannot say.
func Changes(dir string) []string {
	if dir == "" {
		return nil
	}
	base, found := branchPoint(dir)
	if !found {
		return nil
	}
	listed, ran := git(dir, "diff", "--name-only", base, "HEAD")
	if !ran {
		return nil
	}
	var changed []string
	for _, line := range strings.Split(listed, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			changed = append(changed, name)
		}
	}
	return changed
}

// branchPoint is the commit this branch grew from, which is what makes the comparison the change
// rather than the whole history of the repository.
func branchPoint(dir string) (string, bool) {
	for _, name := range theDefaultBranch {
		if _, ran := git(dir, "rev-parse", "--verify", "--quiet", name); !ran {
			continue
		}
		point, ran := git(dir, "merge-base", "HEAD", name)
		if !ran || point == "" {
			continue
		}
		return point, true
	}
	return "", false
}

// git runs one read only command and says whether it worked.
func git(dir string, args ...string) (string, bool) {
	ctx, stop := context.WithTimeout(context.Background(), reading)
	defer stop()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	out, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
