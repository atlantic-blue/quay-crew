package sandbox

import (
	"strings"
	"testing"
)

// The three properties that matter: it clones once per container, the remote is an argument rather than
// part of the script, and no credential value appears in the command at all.
func TestACloneKeepsTheRemoteOutOfTheScriptAndTheTokenOutOfEverything(t *testing.T) {
	const remote = "https://github.com/atlantic-blue/quay-crew.git"
	spec, err := CloneSpec(remote, WorkingPath)
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
	if !strings.Contains(script, "-d") || !strings.Contains(script, ".git") {
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

	// Into a directory of its own: the working directory already holds the memory file the model reads,
	// and git refuses to clone into somewhere that is not empty.
	target := spec.Argv[len(spec.Argv)-1]
	if target != WorkingPath+"/quay-crew" {
		t.Errorf("it clones into %q, want a directory of its own under the working directory", target)
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
