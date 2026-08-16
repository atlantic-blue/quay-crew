package controlplane

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every script the crew runs at sandbox birth is valid shell.
//
// The sandbox swallows a failure here on purpose, because a sandbox that cannot be told to sign
// should not take the conversation down with it, so a script with a typo in it configures nothing
// and says nothing. The integration tests catch that by making a commit, and they need a container
// runtime. This one needs a shell, so it runs everywhere and fails on the same typo in a second.
func TestEverySigningScriptIsValidShell(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("this test reads a script with sh, which is not on this machine")
	}
	for name, script := range map[string]string{
		"no key":           signingOff,
		"an ssh key":       signingSetup,
		"a gpg key":        openPGPSigningSetup(false),
		"a gpg passphrase": openPGPSigningSetup(true),
	} {
		at := filepath.Join(t.TempDir(), "script.sh")
		if err := os.WriteFile(at, []byte(script), 0o600); err != nil {
			t.Fatalf("write the script: %v", err)
		}
		read := exec.Command(shell, "-n", at)
		var complained strings.Builder
		read.Stderr = &complained
		if err := read.Run(); err != nil {
			t.Errorf("the script for %s is not valid shell: %v: %s\n%s", name, err, complained.String(), script)
		}
	}
}

// The gpg script points git at the key it imported, and puts gpg where it cannot ask a question.
//
// Run here rather than only in a container, because the part that goes wrong quietly is reading the
// fingerprint out of gpg's listing: get it wrong and the script stops before it configures anything,
// the sandbox swallows the failure, and the first anybody knows is an unsigned commit. A container
// proves the whole path and needs a daemon; this proves the reading and needs a shell and git.
//
// gpg is a double, written as a program on the path, because the script calls gpg by name and there
// is no other way to answer it. What a real gpg does with a real key is
// TestASessionCanMakeAnOpenPGPCommitThatVerifies.
func TestTheGPGScriptPointsGitAtTheImportedKey(t *testing.T) {
	const fingerprint = "8C4A7B2D1E5F3A9C6B0D4E8F2A1C7B3D5E9F0A6C"

	for _, withPassphrase := range []bool{false, true} {
		home := aSandboxLikeHome(t, fingerprint)
		ran := exec.Command("sh", "-c", openPGPSigningSetup(withPassphrase))
		ran.Env = append(os.Environ(),
			"PATH="+filepath.Join(home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
			"HOME="+home,
			"GNUPGHOME="+filepath.Join(home, "keys"),
			"GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"),
		)
		var complained strings.Builder
		ran.Stderr = &complained
		if err := ran.Run(); err != nil {
			t.Fatalf("the script failed: %v: %s", err, complained.String())
		}

		for name, want := range map[string]string{
			"user.signingkey": fingerprint,
			"gpg.format":      "openpgp",
			"commit.gpgsign":  "true",
			"tag.gpgsign":     "true",
		} {
			if got := gitAsked(t, home, name); got != want {
				t.Errorf("with a passphrase %v, git was left with %s = %q, want %q", withPassphrase, name, got, want)
			}
		}

		options, err := os.ReadFile(filepath.Join(home, "keys", "gpg.conf"))
		if err != nil {
			t.Fatalf("read the gpg configuration the script wrote: %v", err)
		}
		for _, want := range []string{"batch", "no-tty"} {
			if !strings.Contains(string(options), want) {
				t.Errorf("gpg was not told %q, so a task could wait on a prompt:\n%s", want, options)
			}
		}
		if got := strings.Contains(string(options), "passphrase-file"); got != withPassphrase {
			t.Errorf("gpg points at a passphrase file: %v, want %v:\n%s", got, withPassphrase, options)
		}
	}
}

// aSandboxLikeHome is a throwaway home with a gpg on its path that answers the two questions the
// script asks: it takes an import, and it lists one secret key with the given fingerprint.
func aSandboxLikeHome(t *testing.T, fingerprint string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatalf("make a path to put gpg on: %v", err)
	}
	listing := "sec:u:255:22:0000000000000000:1755300000:::u:::scESC:::+:::23::0:\n" +
		"fpr:::::::::" + fingerprint + ":\n" +
		"uid:u::::1755300000::0000000000000000000000000000000000000000::operator <operator@example.com>::::::::::0:\n"
	double := "#!/bin/sh\nfor each in \"$@\"; do\n  if [ \"$each\" = \"--list-secret-keys\" ]; then\n    printf '%s' '" +
		listing + "'\n  fi\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(home, "bin", "gpg"), []byte(double), 0o700); err != nil {
		t.Fatalf("write the gpg double: %v", err)
	}
	return home
}

// gitAsked reads back a setting the script wrote, from git itself rather than from the file, so the
// answer is the one git would give the session.
func gitAsked(t *testing.T, home, name string) string {
	t.Helper()
	ran := exec.Command("git", "config", "--global", "--get", name)
	ran.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"))
	out, err := ran.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
