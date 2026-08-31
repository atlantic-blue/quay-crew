package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Reading the change is the half of the gate that cannot be a table, so it is proved against a real
// repository. What it has to get right is the comparison: the files this branch changed, not every
// file in the repository, or the gate refuses a pull request over infrastructure somebody else wrote.
func TestChangesReadsWhatThisBranchChanged(t *testing.T) {
	dir := repository(t)
	write(t, dir, "docs/README.md", "the service")
	commit(t, dir, "docs: the service")
	run(t, dir, "checkout", "-b", "519-feat-the-stack")
	write(t, dir, "infra/main.tf", `resource "aws_s3_bucket" "site" {}`)
	commit(t, dir, "feat: the stack")

	changed := Changes(dir)
	if len(changed) != 1 || changed[0] != "infra/main.tf" {
		t.Fatalf("read %v, and this branch changed infra/main.tf and nothing else", changed)
	}
	if built := Infrastructure(changed); len(built) != 1 {
		t.Errorf("the gate does not read %v as infrastructure, so it would ask nothing", changed)
	}
}

// A file that was already there on the branch this merges into is not this change, and a pull request
// is not refused over it.
func TestInfrastructureSomebodyElseWroteIsNotThisChange(t *testing.T) {
	dir := repository(t)
	write(t, dir, "infra/main.tf", `resource "aws_s3_bucket" "site" {}`)
	commit(t, dir, "feat: the stack")
	run(t, dir, "checkout", "-b", "520-docs-a-page")
	write(t, dir, "docs/README.md", "the service")
	commit(t, dir, "docs: a page")

	if built := Infrastructure(Changes(dir)); len(built) != 0 {
		t.Errorf("the gate read %v as this change, and this change is a page of documentation", built)
	}
}

// Every failure answers with no files, because a gate that refuses what it cannot read refuses the
// work, and this hook fires on a command every role runs on every slice.
func TestADirectoryGitCannotReadAnswersWithNothing(t *testing.T) {
	if changed := Changes(t.TempDir()); changed != nil {
		t.Errorf("a directory that is not a repository answered %v", changed)
	}
	if changed := Changes(""); changed != nil {
		t.Errorf("no directory at all answered %v", changed)
	}
	if changed := Changes(filepath.Join(t.TempDir(), "gone")); changed != nil {
		t.Errorf("a directory that is not there answered %v", changed)
	}
}

// repository is a git repository with one commit on its default branch, or a skipped test where there
// is no git to make one with.
func repository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git here, and reading a change is what git does")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	run(t, dir, "config", "user.email", "gate@example.com")
	run(t, dir, "config", "user.name", "the gate")
	// Signing is the operator's, and a fixture repository has no key. This turns it off for these
	// three commits and for nothing else.
	run(t, dir, "config", "commit.gpgsign", "false")
	write(t, dir, "start.md", "start")
	commit(t, dir, "chore: start")
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", message)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
