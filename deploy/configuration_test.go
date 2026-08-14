package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestNothingSendsTheOperatorToACheckoutEnvFile guards the whole class rather than the lines that were
// changed. The path was named in a readme, two documents, two error strings and a makefile, so a fix
// that only edited the ones somebody remembered would leave the instruction alive somewhere else.
//
// The changelog is exempt: it records what shipped on the day it shipped, and rewriting it would make
// it a worse record.
func TestNothingSendsTheOperatorToACheckoutEnvFile(t *testing.T) {
	tracked, err := exec.Command("git", "-C", "..", "ls-files").Output()
	if err != nil {
		t.Skipf("not a checkout, so there is nothing to scan: %v", err)
	}

	for _, path := range strings.Split(strings.TrimSpace(string(tracked)), "\n") {
		switch {
		case path == "", path == "CHANGELOG.md", strings.HasPrefix(path, "gen/"):
			continue
		case path == filepath.Join("deploy", "configuration_test.go"):
			continue
		}
		body, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			continue
		}
		if strings.Contains(string(body), "deploy/.env") {
			t.Errorf("%s still sends the operator to deploy/.env, which an installed crew does not have", path)
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
