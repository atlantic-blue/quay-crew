package sandbox

import (
	"strings"
	"testing"
)

// The three properties that matter: it clones once per container, the remote is an argument rather than
// part of the script, and no credential value appears in the command at all.
func TestACloneKeepsTheRemoteOutOfTheScriptAndTheTokenOutOfEverything(t *testing.T) {
	const remote = "https://github.com/atlantic-blue/quay-crew.git"
	spec, err := CloneSpec(remote, SharedPath)
	if err != nil {
		t.Fatalf("CloneSpec: %v", err)
	}
	if len(spec.Argv) < 6 || spec.Argv[0] != "sh" || spec.Argv[1] != "-c" {
		t.Fatalf("the command is %v, want a shell running a script with positional arguments", spec.Argv)
	}
	script, arguments := spec.Argv[2], spec.Argv[3:]

	// The remote is read back as a positional argument, so nothing in it is ever parsed as syntax. A
	// remote in the script itself could end the command and start another one.
	if strings.Contains(script, remote) {
		t.Errorf("the remote is inside the script, where its contents would be read as syntax: %q", script)
	}
	if !strings.Contains(script, `"$1"`) || !strings.Contains(script, `"$2"`) {
		t.Errorf("the script does not read its remote and target as arguments: %q", script)
	}
	found := false
	for _, argument := range arguments {
		if argument == remote {
			found = true
		}
	}
	if !found {
		t.Errorf("the remote is not passed as an argument at all: %v", arguments)
	}

	// Once per container. A sandbox is adopted across turns, and a clone that runs every turn either
	// fails or throws away what the last one did.
	// The guard, not the flag it happens to use: a worktree's .git is a file rather than a directory, so
	// both of these test for existence rather than for a directory.
	if !strings.Contains(script, `"$2/.git"`) || !strings.Contains(script, "||") {
		t.Errorf("the script clones unconditionally, so a second turn would clone again: %q", script)
	}

	// The credential is read from the environment by git, at the moment it asks. Nothing in the command
	// carries a value: an argument list is readable by anything that can inspect the container.
	joined := strings.Join(spec.Argv, " ")
	if !strings.Contains(joined, "$"+CredentialEnv) {
		t.Errorf("the clone never reads the credential from the environment: %q", joined)
	}
	// Named, not just present. A helper handed to git with no key is a config value with no name, and git
	// refuses to parse the command line at all: it was written that way once and only a real run caught it.
	if !strings.Contains(script, "credential.helper=") {
		t.Errorf("the helper is not given as credential.helper, so git cannot parse it: %q", script)
	}
	for _, leaked := range []string{"ghp_", "x-access-token:"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("the command carries %q, which publishes a credential: %q", leaked, joined)
		}
	}

	// Into the workspace's volume, which every session in the workspace shares, so a second conversation
	// costs no second copy of the history.
	target := spec.Argv[len(spec.Argv)-1]
	if target != SharedPath+"/"+RepositoriesDir+"/quay-crew" {
		t.Errorf("it clones into %q, want the workspace's volume", target)
	}
	if strings.HasPrefix(target, WorkingPath) {
		t.Errorf("it clones into %q, which belongs to one session: every session in the workspace would get its own copy", target)
	}
}

// A worktree rather than the shared checkout, because git allows one working tree per branch: two
// sessions in one directory share an index, and the first checkout moves the ground under the other.
func TestEachSessionGetsItsOwnWorkingTreeOnItsOwnBranch(t *testing.T) {
	clone := SharedPath + "/" + RepositoriesDir + "/quay-crew"
	first := WorktreeSpec(clone, WorktreePath("aaaa1111", "quay-crew"),
		WorkingPath+"/quay-crew", SessionBranch("aaaa1111"))
	// The second session's command, to compare against the first: the same link in its own container,
	// and a registered path and a branch of its own.
	second := WorktreeSpec(clone, WorktreePath("bbbb2222", "quay-crew"),
		WorkingPath+"/quay-crew", SessionBranch("bbbb2222"))
	if strings.Join(first.Argv, " ") == strings.Join(second.Argv, " ") {
		t.Error("two sessions are given the identical command, so they cannot have separate trees")
	}

	script, arguments := first.Argv[2], first.Argv[3:]
	if !strings.Contains(script, "worktree add") {
		t.Errorf("it does not make a worktree: %q", script)
	}
	// Pruned first, or a session whose directory was cleared away leaves the clone believing a worktree
	// is still there and refusing to make it again.
	if !strings.Contains(script, "worktree prune") {
		t.Errorf("it never prunes, so a cleared session directory would leave a registration behind: %q", script)
	}
	// Only when it is not already there, because every turn asks.
	if !strings.Contains(script, "-e") {
		t.Errorf("it makes a worktree unconditionally: %q", script)
	}
	for _, want := range []string{clone, WorktreePath("aaaa1111", "quay-crew"), WorkingPath + "/quay-crew", "quay/aaaa1111"} {
		found := false
		for _, argument := range arguments {
			if argument == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not among the arguments %v", want, arguments)
		}
	}

	// Two sessions, two branches. The same branch in two worktrees is the one thing git refuses.
	if SessionBranch("aaaa1111") == SessionBranch("bbbb2222") {
		t.Error("two sessions would be given the same branch, which git will not allow in two worktrees")
	}
	// The registered path differs per session. It is the same string in every container otherwise, and a
	// clone's record of its working trees is shared: two sessions registering one path means the second
	// prunes the first and leaves it holding a tree its clone has forgotten.
	if WorktreePath("aaaa1111", "quay-crew") == WorktreePath("bbbb2222", "quay-crew") {
		t.Error("two sessions would register the same working tree path in one shared clone")
	}
	if !strings.HasPrefix(WorktreePath("aaaa1111", "quay-crew"), SharedPath+"/") {
		t.Errorf("the working tree is at %q, which is not a path every container agrees on",
			WorktreePath("aaaa1111", "quay-crew"))
	}
	// And it is linked into the session's working directory, which is where the model starts.
	if !strings.Contains(strings.Join(first.Argv, " "), WorkingPath+"/quay-crew") {
		t.Errorf("nothing puts it where the model will look: %v", first.Argv)
	}
}

func TestTheDirectoryAcloneLandsIn(t *testing.T) {
	for _, one := range []struct{ remote, want string }{
		{"https://github.com/atlantic-blue/quay-crew.git", "quay-crew"},
		{"https://github.com/atlantic-blue/quay-crew", "quay-crew"},
		{"https://github.com/atlantic-blue/quay-crew/", "quay-crew"},
		{"git@github.com:atlantic-blue/quay-crew.git", "quay-crew"},
	} {
		got, err := RepositoryName(one.remote)
		if err != nil {
			t.Errorf("RepositoryName(%q): %v", one.remote, err)
			continue
		}
		if got != one.want {
			t.Errorf("RepositoryName(%q) = %q, want %q", one.remote, got, one.want)
		}
	}
}

func TestARemoteThatIsNotOneIsRefused(t *testing.T) {
	for _, one := range []struct {
		remote string
		says   string
	}{
		{"", "required"},
		{"https://github.com/x; rm -rf /", "whitespace"},
		{"not-a-remote", "not a repository address"},
		{"ftp://example.com/thing.git", "not a repository address"},
		{"https://", "no host and path"},
		{"https://github.com/", "no host and path"},
		// A credential in the address would be a credential in the database and in every listing that
		// prints a project.
		{"https://someone:hunter2@github.com/a/b.git", "carries a user in it"},
		{" https://github.com/a/b.git", "space around it"},
	} {
		err := UsableRemote(one.remote)
		if err == nil {
			t.Errorf("accepted %q", one.remote)
			continue
		}
		if !strings.Contains(err.Error(), one.says) {
			t.Errorf("refusing %q says %q, want it to say %q", one.remote, err, one.says)
		}
	}
}

// A remote that reaches a clone has already been refused if it is not usable, so CloneSpec says so
// rather than building something.
func TestCloneSpecRefusesWhatUsableRemoteRefuses(t *testing.T) {
	if _, err := CloneSpec("not-a-remote", WorkingPath); err == nil {
		t.Error("CloneSpec built a command for a remote that is not one")
	}
}
