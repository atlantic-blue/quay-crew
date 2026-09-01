package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A system keeps everything it owns in one directory, ~/.krewe. It used to be three: the data under
// ~/.quaycrew/data, the tool's own files under ~/.config/quay, and configuration in the checkout. It
// was then ~/.quay, under the name the product had before this one. These cases hold it to one, and
// to saying so when it finds any of the ones that went.

func TestEverythingASystemOwnsSitsUnderOneDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KREWE_HOME", home)
	t.Setenv("QUAY_HOME", "")

	where, err := kreweHome()
	if err != nil {
		t.Fatalf("quayHome: %v", err)
	}
	if where != home {
		t.Fatalf("the system's directory is %q, want %q", where, home)
	}

	token := systemToken(func(key string) string {
		if key == "KREWE_HOME" {
			return home
		}
		return ""
	}, func(path string) ([]byte, error) {
		want := filepath.Join(home, "data", "system.token")
		if path != want {
			t.Errorf("the token was looked for at %q, want %q", path, want)
		}
		return []byte("a-token"), nil
	})
	if token != "a-token" {
		t.Fatalf("the token read as %q", token)
	}
}

// The default has to be ~/.krewe itself, because a variable set in a test would hide a default that
// still pointed at one of the retired directories. The command a person types is krewe, so the
// directory beside it says krewe.
func TestTheDefaultDirectoryIsKreweInTheHomeDirectory(t *testing.T) {
	t.Setenv("KREWE_HOME", "")
	t.Setenv("QUAY_HOME", "")

	where, err := kreweHome()
	if err != nil {
		t.Fatalf("kreweHome: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory here: %v", err)
	}
	if where != filepath.Join(home, ".krewe") {
		t.Fatalf("the system's directory defaults to %q, want %q", where, filepath.Join(home, ".krewe"))
	}
	for _, retired := range []string{".quaycrew", ".quay", filepath.Join(".config", "quay")} {
		if strings.Contains(where, retired) {
			t.Fatalf("the system's directory is still %q, which holds the retired %q", where, retired)
		}
	}
}

// The way off the variable, which is the half that gets forgotten. QUAY_HOME is in shell profiles, in
// scripts and in service files, and a build that stopped reading it would take an operator who set it
// to a fresh directory: a new token, no sealing key, and every conversation apparently gone. It is
// read for one release.
func TestTheVariableThatWentStillNamesTheDirectory(t *testing.T) {
	theirs := t.TempDir()
	t.Setenv("KREWE_HOME", "")
	t.Setenv("QUAY_HOME", theirs)

	where, err := kreweHome()
	if err != nil {
		t.Fatalf("kreweHome: %v", err)
	}
	if where != theirs {
		t.Fatalf("an operator who set QUAY_HOME is sent to %q, and their system is in %q", where, theirs)
	}
}

// And the new one wins where both are set, or an operator moving off the old variable would keep
// being sent back by whatever still exports it.
func TestTheNewVariableWinsWhereBothAreSet(t *testing.T) {
	theirs, stale := t.TempDir(), t.TempDir()
	t.Setenv("KREWE_HOME", theirs)
	t.Setenv("QUAY_HOME", stale)

	where, err := kreweHome()
	if err != nil {
		t.Fatalf("kreweHome: %v", err)
	}
	if where != theirs {
		t.Fatalf("the system's directory is %q, and KREWE_HOME says %q", where, theirs)
	}
}

// The way off the directory. Everything an operator cannot replace is in it: the system token, the
// driver token, the sealing key that unseals every secret, and every conversation. Starting beside it
// is the failure, not a message they can ignore.
func TestASystemStillInTheDirectoryThatWentIsToldExactlyWhatToMove(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".quay", "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	krewe := filepath.Join(home, ".krewe")

	err := refuseTheOldLayout(home, krewe)

	if err == nil {
		t.Fatal("a system whose things are all in the directory that went was started as though it were new")
	}
	want := "mv " + filepath.Join(home, ".quay") + " " + krewe
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal never says to run\n  %s\nit says:\n%s", want, err.Error())
	}
	// And never a mkdir before it. `mkdir -p ~/.krewe` then `mv ~/.quay ~/.krewe` puts the whole
	// directory inside the new one, a level below anything that looks for it, and the operator is left
	// with a system that still cannot find its own token.
	if strings.Contains(err.Error(), "mkdir") {
		t.Errorf("the refusal says to make the directory first, which buries the old one inside it:\n%s", err.Error())
	}
}

// The state make config leaves, and the one the old check could not see. It writes the directory and
// the configuration file before anything starts, so a system beside a full retired directory has a new
// directory that exists and holds nothing. Reading existence alone started it there, on a fresh token.
func TestADirectoryThatOnlyExistsIsNotASystem(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".quay", "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	krewe := filepath.Join(home, ".krewe")
	if err := os.MkdirAll(krewe, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(krewe, "env"), []byte("QC_MODEL=echo\n"), 0o644); err != nil {
		t.Fatalf("write the configuration file: %v", err)
	}

	if err := refuseTheOldLayout(home, krewe); err == nil {
		t.Fatal("a system was started in an empty directory beside the one holding every conversation")
	}
}

// The other half: once the move is done, nothing is said. A refusal an operator cannot clear is worse
// than no refusal at all, and this is the case that says the guard is passable.
func TestASystemThatHasMovedStarts(t *testing.T) {
	home := t.TempDir()
	krewe := filepath.Join(home, ".krewe")
	if err := os.MkdirAll(filepath.Join(krewe, "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := refuseTheOldLayout(home, krewe); err != nil {
		t.Fatalf("a system that has already moved was refused: %v", err)
	}
}

// The way off the old layout, which is the half that gets forgotten. A system made before the move has
// a gigabyte of transcripts and three tokens in the old place. Reading the new one would start it as
// an empty system with a different token, and every conversation would look lost.
// The command it prints has to be the command that works. Asserting only that a refusal mentions the
// old directory and the word mv passes just as happily on `mv ~/.quaycrew ~/.quay/.quaycrew`, which
// buries the data one level deeper than anything looks for it and leaves the operator with a system
// that still cannot find its own token. So each case asserts the whole line, both halves.
func TestASystemInTheOldLayoutIsToldExactlyWhatToMove(t *testing.T) {
	for _, tc := range []struct {
		name string
		//nolint:revive // stale is the file that exists, want is the line it must produce.
		stale string
		to    string
	}{
		{name: "the data directory", stale: filepath.Join(".quaycrew", "data"), to: "data"},
		{name: "the address you are in", stale: filepath.Join(".config", "quay", "context"), to: "context"},
		{name: "the panel's view", stale: filepath.Join(".config", "quay", "panel-view"), to: "panel-view"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			krewe := filepath.Join(home, ".krewe")
			if err := os.MkdirAll(filepath.Join(home, tc.stale), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			err := refuseTheOldLayout(home, krewe)

			if err == nil {
				t.Fatal("a system sitting in the old layout was started as though it were new")
			}
			want := "mv " + filepath.Join(home, tc.stale) + " " + filepath.Join(krewe, tc.to)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal never says to run\n  %s\nit says:\n%s", want, err.Error())
			}
		})
	}
}

// Only what is actually there is named. A system that kept its context but never opened the panel gets
// two lines, not three, and a line telling somebody to move a file they do not have is a line that
// makes them doubt the other two.
func TestOnlyTheFilesThatExistAreNamed(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".quaycrew", "data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := refuseTheOldLayout(home, filepath.Join(home, ".krewe"))

	if err == nil {
		t.Fatal("a system with data in the old place was not stopped")
	}
	if strings.Contains(err.Error(), "panel-view") {
		t.Errorf("it names a file this system never had:\n%s", err.Error())
	}
	if moves := strings.Count(err.Error(), "mv "); moves != 1 {
		t.Errorf("it prints %d moves for one directory:\n%s", moves, err.Error())
	}
}

// Nothing is said once the move is done, and nothing is said to somebody who never had the old
// layout. A refusal that fires on a system with nothing to move is a refusal nobody can clear.
func TestASystemWithNothingToMoveIsNotStopped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(home string)
	}{
		{name: "a system that never had the old layout", build: func(string) {}},
		{
			name: "a system that has already moved",
			build: func(home string) {
				for _, dir := range []string{".quaycrew", filepath.Join(".config", "quay"), ".quay"} {
					if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
						panic(err)
					}
				}
				// What moving means: the things themselves are in the new directory. The retired ones
				// are left standing, because an operator removes those in their own time.
				if err := os.MkdirAll(filepath.Join(home, ".krewe", "data"), 0o755); err != nil {
					panic(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.build(home)

			if err := refuseTheOldLayout(home, filepath.Join(home, ".krewe")); err != nil {
				t.Fatalf("a system with nothing to move was refused: %v", err)
			}
		})
	}
}
