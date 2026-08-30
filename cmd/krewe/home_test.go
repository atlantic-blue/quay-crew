package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A system keeps everything it owns in one directory. It used to be three: the data under
// ~/.quaycrew/data, the tool's own files under ~/.config/quay, and configuration in the checkout.
// These cases hold it to one, and to saying so when it finds one of the old ones.

func TestEverythingASystemOwnsSitsUnderOneDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("QUAY_HOME", home)

	where, err := quayHome()
	if err != nil {
		t.Fatalf("quayHome: %v", err)
	}
	if where != home {
		t.Fatalf("the system's directory is %q, want %q", where, home)
	}

	token := systemToken(func(key string) string {
		if key == "QUAY_HOME" {
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

// The default has to be ~/.quay itself, because QUAY_HOME set in a test would hide a default that
// still pointed at either of the old directories.
func TestTheDefaultDirectoryIsQuayInTheHomeDirectory(t *testing.T) {
	t.Setenv("QUAY_HOME", "")

	where, err := quayHome()
	if err != nil {
		t.Fatalf("quayHome: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory here: %v", err)
	}
	if where != filepath.Join(home, ".quay") {
		t.Fatalf("the system's directory defaults to %q, want %q", where, filepath.Join(home, ".quay"))
	}
	for _, retired := range []string{".quaycrew", filepath.Join(".config", "quay")} {
		if strings.Contains(where, retired) {
			t.Fatalf("the system's directory is still %q, which holds the retired %q", where, retired)
		}
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
			quay := filepath.Join(home, ".quay")
			if err := os.MkdirAll(filepath.Join(home, tc.stale), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			err := refuseTheOldLayout(home, quay)

			if err == nil {
				t.Fatal("a system sitting in the old layout was started as though it were new")
			}
			want := "mv " + filepath.Join(home, tc.stale) + " " + filepath.Join(quay, tc.to)
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

	err := refuseTheOldLayout(home, filepath.Join(home, ".quay"))

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
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.build(home)

			if err := refuseTheOldLayout(home, filepath.Join(home, ".quay")); err != nil {
				t.Fatalf("a system with nothing to move was refused: %v", err)
			}
		})
	}
}
