package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Configuration that lives inside the checkout cannot be given to anybody: a crew that was installed
// rather than cloned has no checkout to put it in. These cases hold the stack to that.

// TestTheStackIsToldWhereItsConfigurationIs.
//
// Compose loads a `.env` sitting beside its own compose file with nothing asked of it, which is how
// the configuration ended up inside the checkout in the first place. Being told the path explicitly is
// what makes the location a decision rather than an accident of where the file happens to be.
func TestTheStackIsToldWhereItsConfigurationIs(t *testing.T) {
	printed := makeVariable(t, "COMPOSE")

	if !strings.Contains(printed, "--env-file") {
		t.Fatalf("compose is not told where its configuration is, so it reads whatever sits beside the compose file:\n%s", printed)
	}
	if strings.Contains(printed, "deploy/.env") {
		t.Fatalf("compose is pointed back inside the checkout:\n%s", printed)
	}
}

// TestTheConfigurationPathIsOutsideTheCheckout, and sits under QUAY_HOME, which is where a crew keeps
// what belongs to it on this machine.
func TestTheConfigurationPathIsOutsideTheCheckout(t *testing.T) {
	printed := makeVariable(t, "ENV_FILE")

	if printed == "" {
		t.Fatal("the makefile says nothing about where configuration lives")
	}
	if !filepath.IsAbs(printed) {
		t.Fatalf("configuration is at %q, which is relative, so it resolves inside whatever checkout make was run from", printed)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if strings.HasPrefix(printed, root+string(filepath.Separator)) {
		t.Fatalf("configuration is at %q, which is inside the checkout at %q", printed, root)
	}
}

// TestNothingSendsTheOperatorToARetiredLocation guards the whole class rather than the lines that were
// changed. Each of these paths was named across a readme, several documents, error strings, a makefile,
// a compose file and the continuous integration workflow, so a fix that only edited the ones somebody
// remembered would leave the instruction alive somewhere else.
//
// Exemptions are per path rather than per file, because the places that name a retired directory on
// purpose are the ones that refuse it, and they name only that one. Exempting a whole file instead
// would stop it being scanned for the others: the tool has to keep naming ~/.quaycrew in the refusal
// that moves a crew off the old layout, and still must never send anybody back to a checkout env file.
//
// The changelog is exempt from all of them. It records what shipped on the day it shipped, and
// rewriting it would make it a worse record.
func TestNothingSendsTheOperatorToARetiredLocation(t *testing.T) {
	home := filepath.Join("cmd", "quay", "home.go")
	homeTest := filepath.Join("cmd", "quay", "home_test.go")
	itself := filepath.Join("deploy", "configuration_test.go")

	// The directory, not the product's name: com.quaycrew.build is a docker label and stays.
	oneCrewDirectory := []string{home, homeTest, itself}
	retired := []struct {
		path    string
		because string
		allowed []string
	}{
		{
			path:    "deploy/.env",
			because: "an installed crew has no checkout to hold it",
			allowed: []string{itself},
		},
		{path: ".quaycrew/", because: "a crew keeps everything it owns in ~/.quay", allowed: oneCrewDirectory},
		{path: `".quaycrew"`, because: "a crew keeps everything it owns in ~/.quay", allowed: oneCrewDirectory},
		{path: ".config/quay", because: "a crew keeps everything it owns in ~/.quay", allowed: oneCrewDirectory},
	}

	tracked, err := exec.Command("git", "-C", "..", "ls-files").Output()
	if err != nil {
		t.Skipf("not a checkout, so there is nothing to scan: %v", err)
	}

	for _, path := range strings.Split(strings.TrimSpace(string(tracked)), "\n") {
		if path == "" || path == "CHANGELOG.md" || strings.HasPrefix(path, "gen/") {
			continue
		}
		body, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			continue
		}
		for _, one := range retired {
			if slices.Contains(one.allowed, path) || !strings.Contains(string(body), one.path) {
				continue
			}
			t.Errorf("%s still sends the operator to %s, and %s", path, one.path, one.because)
		}
	}
}

// makeVariable asks make what a variable expands to, so these read the real recipe rather than a
// remembered copy of it. It takes the variables a case needs set, so a case can ask what a path
// expands to under a crew directory of its own rather than under the operator's.
func makeVariable(t *testing.T, name string, with ...string) string {
	t.Helper()
	out, err := exec.Command("make", append([]string{"-C", "..", "--no-print-directory", "print-" + name}, with...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("make print-%s: %v\n%s", name, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The stack mounts $(QUAY_HOME)/data, which holds the crew's token, the key that seals every secret
// and the skills a session is given. Starting with that directory missing or empty is not an error
// the operator ever sees as one: the control plane mints a fresh token and the crew comes up looking
// exactly like one that lost every conversation. `home-check` is the refusal that stops it.
//
// There are three states and the middle one is the only one that matters. The outer two are how a
// guard like this gets written wrong. A guard that refuses whenever the data directory is absent
// breaks the first run on a clean machine, where absent is correct. A guard whose condition can
// never be true passes every case that only asks whether the happy path still works, which is how
// the previous one sat here dead after the directory it watched was removed from every machine.

// TestACleanMachineStarts. Nothing has ever run here, so there is no crew to have lost, and `up`
// creates the data directory a moment later in `config`.
func TestACleanMachineStarts(t *testing.T) {
	quay := filepath.Join(t.TempDir(), ".quay")

	out, err := homeCheck(t, quay)

	if err != nil {
		t.Fatalf("a machine that never held a crew was refused its first run: %v\n%s", err, out)
	}
}

// TestACrewWhoseDataWentMissingIsRefused, in both shapes the loss takes.
//
// Empty is the shape it is in by the time anybody types `make up` again: `config` runs `mkdir -p` on
// the data directory as a prerequisite of `down`, `logs`, `ps` and `up-check`, so a directory that
// was deleted is back, and empty, after any one of those. A guard that only watched for a missing
// directory would be dead again within one command.
func TestACrewWhoseDataWentMissingIsRefused(t *testing.T) {
	losses := []struct {
		name string
		take func(t *testing.T, data string)
	}{
		{
			name: "the directory was deleted",
			take: func(t *testing.T, data string) {
				if err := os.RemoveAll(data); err != nil {
					t.Fatalf("remove: %v", err)
				}
			},
		},
		{
			name: "the directory is back and empty",
			take: func(t *testing.T, data string) {
				entries, err := os.ReadDir(data)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				for _, entry := range entries {
					if err := os.RemoveAll(filepath.Join(data, entry.Name())); err != nil {
						t.Fatalf("remove: %v", err)
					}
				}
			},
		},
	}

	for _, loss := range losses {
		t.Run(loss.name, func(t *testing.T) {
			quay := aCrewThatHasRun(t)
			loss.take(t, filepath.Join(quay, "data"))

			out, err := homeCheck(t, quay)

			if err == nil {
				t.Fatalf("the stack started on a crew whose data is gone:\n%s", out)
			}
			// What is missing, and what to check before starting again. A refusal that only says no
			// is a refusal somebody clears by deleting whatever it named.
			for _, want := range []string{filepath.Join(quay, "data"), "QUAY_HOME is " + quay} {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal never says %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, ".quaycrew") {
				t.Errorf("the refusal sends the operator to a directory that no longer exists:\n%s", out)
			}
		})
	}
}

// TestAHealthyCrewStarts, and is recorded as a crew, because the recording is what the refusal above
// reads. A machine that holds a crew and never writes that down cannot tell a lost one from a first
// run, and the guard is dead in the other direction.
func TestAHealthyCrewStarts(t *testing.T) {
	quay := aCrewThatHasRun(t)

	out, err := homeCheck(t, quay)

	if err != nil {
		t.Fatalf("a crew whose data is where it belongs was refused: %v\n%s", err, out)
	}
	marker := makeVariable(t, "STARTED_FILE", "QUAY_HOME="+quay)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("nothing records that this machine holds a crew, so a later run cannot tell one that "+
			"lost its data from one that never had any: %v", err)
	}
}

// aCrewThatHasRun is the crew directory a machine has after the stack has started: the data
// directory holds the token the control plane mints on its first run, and the guard has seen it.
func aCrewThatHasRun(t *testing.T) string {
	t.Helper()

	quay := filepath.Join(t.TempDir(), ".quay")
	if err := os.MkdirAll(filepath.Join(quay, "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(quay, "data", "crew.token"), []byte("a-token\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := homeCheck(t, quay)
	if err != nil {
		t.Fatalf("a crew whose data is where it belongs was refused: %v\n%s", err, out)
	}
	return quay
}

// homeCheck runs the guard against a crew directory of the test's own, so no case reads or writes
// the operator's.
func homeCheck(t *testing.T, quay string) (string, error) {
	t.Helper()

	out, err := exec.Command("make", "-C", "..", "--no-print-directory",
		"home-check", "QUAY_HOME="+quay).CombinedOutput()
	return string(out), err
}

// TestTheCrewsDirectoryIsMadeBeforeComposeCouldMakeIt.
//
// Docker creates a missing bind mount source itself, as root. The crew's directory now holds the
// files the tool writes as well as the data the stack mounts, so a stack that came up first would
// leave it owned by root and the next `quay use` would fail with permission denied on a path it had
// just been told to write. That is what happened: the tests were green and the composed stack was not.
func TestTheCrewsDirectoryIsMadeBeforeComposeCouldMakeIt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "crew")

	out, err := exec.Command("make", "-C", "..", "--no-print-directory",
		"config", "QUAY_HOME="+home).CombinedOutput()
	if err != nil {
		t.Fatalf("make config: %v\n%s", err, out)
	}

	for _, want := range []string{home, filepath.Join(home, "data")} {
		info, err := os.Stat(want)
		if err != nil {
			t.Fatalf("%s was not made, so docker would make it as root: %v", want, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", want)
		}
	}

	// Writable by whoever ran it, which is the whole point of making it here rather than letting the
	// daemon do it.
	probe := filepath.Join(home, "context")
	if err := os.WriteFile(probe, []byte("me/bills\n"), 0o644); err != nil {
		t.Fatalf("the crew's directory is not writable by the operator: %v", err)
	}
}
