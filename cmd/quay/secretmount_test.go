package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// A git configuration, complete with the blank line and the trailing newline a real one has, because
// this is the shape the whole feature exists for.
const aGitConfig = "[user]\n\tname = operator\n\temail = operator@example.com\n\n[commit]\n\tgpgsign = false\n"

// writtenInto reads back what the session's sandbox was actually asked to write at a path, and
// whether it was asked at all. It reads the commands the sandbox was given rather than anything the
// crew records about its own intent, so a write that never reached a sandbox cannot pass.
//
// The write redirects on its last line, and the value travels in the environment of that one command
// rather than in an argument, which is where both halves are read from.
func writtenInto(boxes *sandbox.FakeProvider, at string) (string, bool) {
	for _, box := range boxes.Boxes {
		for _, spec := range box.Ran {
			if len(spec.Argv) != 3 || spec.Argv[0] != "sh" || spec.Argv[1] != "-c" {
				continue
			}
			lines := strings.Split(strings.TrimSpace(spec.Argv[2]), "\n")
			_, path, redirects := strings.Cut(lines[len(lines)-1], "> ")
			if !redirects || strings.TrimSpace(path) != at {
				continue
			}
			for _, entry := range spec.Env {
				if name, value, found := strings.Cut(entry, "="); found && name == "QC_SECRET_FILE_VALUE" {
					return value, true
				}
			}
		}
	}
	return "", false
}

// The point of the command: a file on disk reaches the session as a file, byte for byte.
func TestAMountedFileReachesTheSessionByteForByte(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, []byte(aGitConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	said := mustRun(t, client, "secret", "mount", "gitconfig", path)
	if !strings.Contains(said, "/run/secrets/gitconfig") {
		t.Fatalf("mounting did not say where it lands: %q", said)
	}
	if strings.Contains(said, "name = operator") {
		t.Fatalf("mounting echoed what is in the file: %q", said)
	}

	mustRun(t, client, "task", "hello")
	got, given := writtenInto(boxes, "/run/secrets/gitconfig")
	if !given {
		t.Fatal("the session was given no file at /run/secrets/gitconfig")
	}
	if got != aGitConfig {
		t.Fatalf("the session was given %q, want the file unchanged", got)
	}
}

// `quay secret set` trims, because a token gains a newline from the tool that printed it. A file's
// bytes are the file, and one that arrives a byte shorter is a file the operator cannot reason about.
func TestMountingDoesNotTrimTheWayASecretDoes(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	saying(t, aGitConfig)
	mustRun(t, client, "secret", "mount", "gitconfig")
	mustRun(t, client, "task", "hello")

	got, _ := writtenInto(boxes, "/run/secrets/gitconfig")
	if got != aGitConfig {
		t.Fatalf("the piped file was changed on the way in: %q", got)
	}
}

// A mounted secret is not also in the environment. That is the whole reason to prefer a file for a
// credential: a container's environment is readable through docker inspect for the life of it.
func TestAMountedSecretIsNotAlsoInTheEnvironment(t *testing.T) {
	client, boxes := aCrewWatchingItsSandboxes(t)

	saying(t, aGitConfig)
	mustRun(t, client, "secret", "mount", "gitconfig")
	mustRun(t, client, "task", "hello")

	if got, given := carried(boxes, "gitconfig"); given {
		t.Fatalf("the session carries gitconfig=%q in its environment", got)
	}
}

// The listing is where an operator finds out which of their secrets a session opens by path. Two
// that arrive in different places and read the same in a listing is the same as not saying.
func TestTheListingSaysWhereAMountedSecretLands(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	// The typed form first, because saying something on standard input makes every later command a
	// piped one for the rest of the test.
	mustRun(t, client, "secret", "set", "GH_TOKEN", "ghp-1234")
	saying(t, aGitConfig)
	mustRun(t, client, "secret", "mount", "gitconfig")

	listed := mustRun(t, client, "secret", "list")
	if !strings.Contains(listed, "gitconfig") || !strings.Contains(listed, "mounted at /run/secrets/gitconfig") {
		t.Fatalf("the listing does not say where the mounted secret lands: %q", listed)
	}
	if strings.Contains(listed, "name = operator") || strings.Contains(listed, "ghp-1234") {
		t.Fatalf("the listing printed a value: %q", listed)
	}
	// The one that goes into the environment is not described as landing anywhere, because it does
	// not: a session reads it without opening anything.
	for _, line := range strings.Split(listed, "\n") {
		if strings.Contains(line, "GH_TOKEN") && strings.Contains(line, "mounted at") {
			t.Fatalf("a secret in the environment is described as mounted: %q", line)
		}
	}
}

// A workspace can be named the same way it is for `quay secret set`, and standing somewhere else must
// not decide where a credential lands.
func TestMountingCanNameItsWorkspace(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "use", "me/house-bills")

	saying(t, aGitConfig)
	mustRun(t, client, "secret", "mount", "elsewhere", "gitconfig")

	if there := mustRun(t, client, "secret", "list", "elsewhere"); !strings.Contains(there, "gitconfig") {
		t.Fatalf("the named workspace did not get it: %q", there)
	}
	if here := mustRun(t, client, "secret", "list", "me"); strings.Contains(here, "gitconfig") {
		t.Fatalf("it landed where we were standing rather than where we said: %q", here)
	}
}

// A path that is not there is the ordinary mistake, and the refusal has to name it rather than
// storing an empty file the listing then reports as set.
func TestMountingAFileThatIsNotThereIsRefused(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	missing := filepath.Join(t.TempDir(), "not-here")
	err := refused(t, client, "secret", "mount", "gitconfig", missing)
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("the refusal does not name the path: %s", err)
	}
	if listed := mustRun(t, client, "secret", "list"); !strings.Contains(listed, "no secrets in this crew") {
		t.Fatalf("something was stored anyway: %q", listed)
	}
}

// An empty file is one that turned out to have nothing in it, and mounting it leaves a session
// opening a credential that says nothing while the listing reports it as set.
func TestMountingAnEmptyFileIsRefused(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refused(t, client, "secret", "mount", "gitconfig", path); !strings.Contains(err.Error(), "was not set") {
		t.Fatalf("an empty file was stored: %s", err)
	}
}

// A name that walks out of its own directory would land the file wherever it said. Refused when it is
// set, not at the moment of writing.
func TestAMountedNameCannotEscapeItsDirectory(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	saying(t, "root")
	if err := refused(t, client, "secret", "mount", "../../etc/passwd"); !strings.Contains(err.Error(), "file name") {
		t.Fatalf("the refusal does not say why: %s", err)
	}
}

// The way onto the command. Somebody who has just read that a credential can be a file needs the
// usage to say how, and a usage that only offers the two old forms is the same as the command not
// being there.
func TestTheUsageOffersMounting(t *testing.T) {
	client, _ := aCrewWatchingItsSandboxes(t)

	if err := refused(t, client, "secret"); !strings.Contains(err.Error(), "quay secret mount") {
		t.Fatalf("the usage does not offer mounting: %s", err)
	}
}
