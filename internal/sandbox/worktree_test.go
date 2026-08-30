package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/skill"
)

// The shape the git brief describes, run against real git rather than read.
//
// A workspace holds one clone and a working tree per session, both in the volume every session in the
// workspace sees. This proves the two things that shape is for: two sessions get their own files out
// of one clone, and neither takes the other's tree away.
func TestOneCloneCarriesAWorkingTreePerSession(t *testing.T) {
	root := t.TempDir()
	origin := makeOrigin(t, filepath.Join(root, "origin"))

	clone := filepath.Join(root, "repos", "example")
	git(t, root, "clone", origin, clone)

	trees := map[string]string{}
	for _, session := range []string{"a1b2c3d4e5f6", "0f1e2d3c4b5a"} {
		tree := filepath.Join(root, "worktrees", session, "example")
		git(t, clone, "worktree", "add", tree, "-b", "krewe/"+session, "origin/HEAD")
		trees[session] = tree
		if _, err := os.Stat(filepath.Join(tree, "README.md")); err != nil {
			t.Fatalf("the working tree for %s does not hold the repository's files: %v", session, err)
		}
	}

	// The second add did not prune the first, which is the whole reason the trees carry the session
	// in their path.
	listed := git(t, clone, "worktree", "list")
	for session, tree := range trees {
		if !strings.Contains(listed, tree) {
			t.Errorf("the clone no longer holds the tree for %s:\n%s", session, listed)
		}
	}

	// A session's files are its own: writing in one tree does not reach the session beside it.
	first, second := trees["a1b2c3d4e5f6"], trees["0f1e2d3c4b5a"]
	if err := os.WriteFile(filepath.Join(first, "notes.md"), []byte("mine"), 0o600); err != nil {
		t.Fatalf("writing in the first tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("the second session sees the first session's file, so the trees are not separate: %v", err)
	}
}

// What the layout avoids. Every session sees the same paths, so two sessions taking a tree at one
// path is one path as far as the clone is concerned, and the second is refused. This is the failure
// the session identifier in the path exists to prevent, and it is here so the reason survives.
func TestTwoSessionsAtOnePathCollide(t *testing.T) {
	root := t.TempDir()
	origin := makeOrigin(t, filepath.Join(root, "origin"))
	clone := filepath.Join(root, "repos", "example")
	git(t, root, "clone", origin, clone)

	shared := filepath.Join(root, "worktrees", "example")
	git(t, clone, "worktree", "add", shared, "-b", "krewe/first", "origin/HEAD")

	out, err := exec.Command("git", "-C", clone, "worktree", "add", shared, "-b", "krewe/second", "origin/HEAD").CombinedOutput()
	if err == nil {
		t.Fatalf("a second session took a tree at the same path and git allowed it:\n%s", out)
	}
}

// The brief is the only thing that tells a session where any of this goes, so it has to name the
// directories the system mounts and the variable it sets. A layout changed here and not there is a
// session cloning into a directory nobody shares.
func TestTheGitBriefNamesTheSharedLayout(t *testing.T) {
	skills, err := skill.Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}
	var brief string
	for _, one := range skills {
		if one.Name == "git" {
			brief = one.Brief
		}
	}
	if brief == "" {
		t.Fatal("skills/ does not hold the git skill, so this test proves nothing")
	}
	// The tree path carries the variable, not only the directory: a brief naming the directory and
	// then some other name for this session is the collision back again.
	for _, named := range []string{ReposPath, WorktreesPath + "/$" + SessionIDEnv} {
		if !strings.Contains(brief, named) {
			t.Errorf("the git brief never names %q, so a session does not know to use it", named)
		}
	}
}

// makeOrigin makes a repository with one commit in it, to clone from.
func makeOrigin(t *testing.T, at string) string {
	t.Helper()
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatalf("making the origin directory: %v", err)
	}
	git(t, at, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(at, "README.md"), []byte("a repository\n"), 0o600); err != nil {
		t.Fatalf("writing the origin's file: %v", err)
	}
	git(t, at, "add", "README.md")
	git(t, at, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false",
		"commit", "-m", "first")
	return at
}

// git runs a command in a directory and fails the test with what it said. A missing git fails here
// rather than skipping: the sandbox image carries it and so does every machine this suite runs on, so
// a skip would report a layout nobody checked.
func git(t *testing.T, in string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = in
	// A working tree is added as whoever runs the test, and their own configuration must not decide
	// what this proves: a template directory, a default branch name or a signing key would each change
	// the result.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), in, err, out)
	}
	return string(out)
}
