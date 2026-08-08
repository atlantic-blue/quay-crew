//go:build integration

package sandbox_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// TestASessionActuallyClonesOnce proves the part the doubles cannot: the real script puts a real checkout
// in the session's working directory, beside the memory file that is already there, and running it again
// leaves the session's own work alone.
//
// The remote is a bare repository made inside the container, so this needs no network and no credential.
// What it does not cover is a private clone over https, which needs a real token against a real host.
func TestAWorkspaceClonesOnceAndEachSessionGetsAWorkingTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to an image with git in it")
	}

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx,
		sandbox.Config{ID: "cloneonce" + strings.Repeat("0", 14)})
	if err != nil {
		t.Fatalf("create the sandbox: %v", err)
	}
	defer func() { _ = box.Close(ctx) }()

	// Something to clone, and a working directory that already holds the memory file, which is why a
	// working tree cannot be checked out into the working directory itself.
	setup := strings.Join([]string{
		"set -e",
		"mkdir -p /tmp/seed && cd /tmp/seed && git init -q",
		"echo first > a.txt && git add a.txt",
		"git -c user.name=a -c user.email=a@b commit -q -m first",
		"git clone -q --bare /tmp/seed /tmp/origin",
		"mkdir -p " + sandbox.WorkingPath + " " + sandbox.SharedPath,
		"echo 'the memory file' > " + sandbox.WorkingPath + "/CLAUDE.md",
	}, "; ")
	if said := execOutput(t, ctx, box, "sh", "-c", setup); strings.TrimSpace(said) != "" {
		t.Logf("setting up said: %s", said)
	}

	clonedTo := sandbox.SharedPath + "/" + sandbox.RepositoriesDir + "/origin"
	clone := func() {
		t.Helper()
		runInSandbox(t, ctx, box, sandbox.Spec{
			Argv: []string{"sh", "-c", sandbox.CloneScript(), "sh", "/tmp/origin", clonedTo},
		}, "clone")
	}
	worktree := func(link, session string) string {
		t.Helper()
		at := sandbox.WorktreePath(session, "origin")
		runInSandbox(t, ctx, box, sandbox.WorktreeSpec(clonedTo, at, link, sandbox.SessionBranch(session)), "worktree")
		return at
	}

	// One clone in the workspace's volume.
	clone()
	if got := execOutput(t, ctx, box, "cat", clonedTo+"/a.txt"); strings.TrimSpace(got) != "first" {
		t.Fatalf("the clone reads %q, want the file that was in the repository", got)
	}

	// Two sessions, each with its own working tree of it.
	firstLink := sandbox.WorkingPath + "/origin"
	secondLink := "/home/agent/second/origin"
	if said := execOutput(t, ctx, box, "mkdir", "-p", "/home/agent/second"); strings.TrimSpace(said) != "" {
		t.Logf("making the second session's directory said: %s", said)
	}
	first := worktree(firstLink, "aaaa1111")
	second := worktree(secondLink, "bbbb2222")

	// Through the link, which is what the model actually opens.
	for _, at := range []string{firstLink, secondLink} {
		if got := execOutput(t, ctx, box, "cat", at+"/a.txt"); strings.TrimSpace(got) != "first" {
			t.Fatalf("the working tree at %s reads %q", at, got)
		}
	}
	// The memory file survived a checkout into the directory beside it.
	if got := execOutput(t, ctx, box, "cat", sandbox.WorkingPath+"/CLAUDE.md"); !strings.Contains(got, "the memory file") {
		t.Fatalf("the memory file reads %q, so the working tree disturbed the working directory", got)
	}

	// Each on its own branch, which is what git demands and what keeps two conversations apart.
	branchOf := func(at string) string {
		return strings.TrimSpace(execOutput(t, ctx, box, "git", "-C", at, "rev-parse", "--abbrev-ref", "HEAD"))
	}
	if branchOf(first) != "quay/aaaa1111" || branchOf(second) != "quay/bbbb2222" {
		t.Fatalf("they are on %q and %q, want a branch each", branchOf(first), branchOf(second))
	}

	// One session committing does not move the other, which is the whole reason for a worktree each.
	commit := "cd " + first + " && echo mine > mine.txt && git add mine.txt && " +
		"git -c user.name=a -c user.email=a@b commit -q -m mine"
	if said := execOutput(t, ctx, box, "sh", "-c", commit); strings.TrimSpace(said) != "" {
		t.Logf("committing said: %s", said)
	}
	if got := execOutput(t, ctx, box, "sh", "-c", "ls "+second); strings.Contains(got, "mine.txt") {
		t.Error("one session's commit appeared in the other's working tree")
	}

	// And asking again changes nothing, because every turn asks.
	if said := execOutput(t, ctx, box, "sh", "-c", "echo local > "+first+"/local-work.txt"); strings.TrimSpace(said) != "" {
		t.Logf("writing the session's own work said: %s", said)
	}
	clone()
	worktree(first, "aaaa1111")
	if got := execOutput(t, ctx, box, "cat", first+"/local-work.txt"); strings.TrimSpace(got) != "local" {
		t.Fatalf("asking again threw away the session's own work: %q", got)
	}
}

// runInSandbox executes one command inside the sandbox and fails the test with what it said.
func runInSandbox(t *testing.T, ctx context.Context, box sandbox.Sandbox, spec sandbox.Spec, what string) {
	t.Helper()
	proc, err := box.Exec(ctx, spec)
	if err != nil {
		t.Fatalf("run the %s: %v", what, err)
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	if err := proc.Wait(); err != nil {
		t.Fatalf("the %s failed: %v: %s", what, err, proc.Stderr())
	}
}
