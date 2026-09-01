package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Configuration that lives inside the checkout cannot be given to anybody: a system that was installed
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

// TestTheConfigurationPathIsOutsideTheCheckout, and sits under KREWE_HOME, which is where a system keeps
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
// would stop it being scanned for the others: the tool has to keep naming ~/.quaycrew in its
// refusal, and still must never send anybody back to a checkout env file.
//
// The changelog is exempt from all of them. It records what shipped on the day it shipped, and
// rewriting it would make it a worse record.
func TestNothingSendsTheOperatorToARetiredLocation(t *testing.T) {
	home := filepath.Join("cmd", "krewe", "home.go")
	homeTest := filepath.Join("cmd", "krewe", "home_test.go")
	itself := filepath.Join("deploy", "configuration_test.go")

	// The directory, not the product's name: com.quaycrew.build is a docker label and stays.
	oneSystemDirectory := []string{home, homeTest, itself}
	// The plan keeps naming the directory that went, the way the changelog does: it is the record of
	// the move rather than an instruction to a reader.
	theRecordOfTheMove := append([]string{"docs/RENAME.md"}, oneSystemDirectory...)
	// The retired variable is read on purpose, for one release, so a shell profile that exports it
	// still points the stack at the operator's own directory. The makefile reads it, the scenario that
	// proves an operator who exports it still lands in their own system sets it, and the entry that
	// announces the move says it is still read.
	readsTheRetiredOne := append([]string{
		"Makefile",
		filepath.Join("features", "systemdirectory_steps_test.go"),
		filepath.Join("changelog.d", "517-the-system-keeps-its-things-in-krewe.md"),
	}, theRecordOfTheMove...)
	retired := []struct {
		path    string
		because string
		allowed []string
	}{
		{
			path:    "deploy/.env",
			because: "an installed system has no checkout to hold it",
			allowed: []string{itself},
		},
		{path: ".quaycrew/", because: "a system keeps everything it owns in ~/.krewe", allowed: oneSystemDirectory},
		{path: `".quaycrew"`, because: "a system keeps everything it owns in ~/.krewe", allowed: oneSystemDirectory},
		{path: ".config/quay", because: "a system keeps everything it owns in ~/.krewe", allowed: oneSystemDirectory},
		{
			path:    "~/.quay/",
			because: "a system keeps everything it owns in ~/.krewe, and the tokens, the sealing key and every conversation are in it",
			allowed: theRecordOfTheMove,
		},
		{
			path:    "QUAY_HOME",
			because: "the variable is KREWE_HOME, and the retired one is read for one release rather than written down",
			allowed: readsTheRetiredOne,
		},
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
// remembered copy of it.
func makeVariable(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("make", "-C", "..", "--no-print-directory", "print-"+name).CombinedOutput()
	if err != nil {
		t.Fatalf("make print-%s: %v\n%s", name, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestTheSystemsDirectoryIsMadeBeforeComposeCouldMakeIt.
//
// Docker creates a missing bind mount source itself, as root. The system's directory now holds the
// files the tool writes as well as the data the stack mounts, so a stack that came up first would
// leave it owned by root and the next `krewe use` would fail with permission denied on a path it had
// just been told to write. That is what happened: the tests were green and the composed stack was not.
func TestTheSystemsDirectoryIsMadeBeforeComposeCouldMakeIt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "system")

	out, err := exec.Command("make", "-C", "..", "--no-print-directory",
		"config", "KREWE_HOME="+home).CombinedOutput()
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
		t.Fatalf("the system's directory is not writable by the operator: %v", err)
	}
}
