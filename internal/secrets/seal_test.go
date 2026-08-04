package secrets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/secrets"
)

func TestSealedValueOpensWithItsKeyAndNothingElse(t *testing.T) {
	key, err := secrets.KeyAt(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("KeyAt: %v", err)
	}
	other, err := secrets.KeyAt(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("KeyAt: %v", err)
	}

	const token = "sk-ant-oat-not-a-real-one"
	sealed, err := secrets.Seal(key, token)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// The whole point: the bytes in the database are worth nothing on their own, and a dump is a thing
	// people paste into messages.
	if strings.Contains(string(sealed), token) {
		t.Fatal("the sealed value carries the secret in the clear")
	}

	got, err := secrets.Open(key, sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != token {
		t.Fatalf("Open = %q, want the token back", got)
	}
	if _, err := secrets.Open(other, sealed); err == nil {
		t.Fatal("a different key opened it, so the key is doing nothing")
	}
}

// TestSealingTheSameValueTwiceLooksDifferent: without a fresh nonce, two workspaces with the same
// token would be visibly the same in a dump, which says something the dump should not.
func TestSealingTheSameValueTwiceLooksDifferent(t *testing.T) {
	key, _ := secrets.KeyAt(filepath.Join(t.TempDir(), "secrets.key"))
	first, _ := secrets.Seal(key, "same")
	second, _ := secrets.Seal(key, "same")
	if string(first) == string(second) {
		t.Fatal("sealing the same value twice produced the same bytes")
	}
}

// TestTheKeyIsMadeOnceAndKeptPrivate: it is made rather than asked for, because a step the operator
// has to perform before anything works is a step that gets skipped.
func TestTheKeyIsMadeOnceAndKeptPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")

	first, err := secrets.KeyAt(path)
	if err != nil {
		t.Fatalf("KeyAt: %v", err)
	}
	if len(first) != secrets.KeyLength {
		t.Fatalf("the key is %d bytes, want %d", len(first), secrets.KeyLength)
	}
	again, err := secrets.KeyAt(path)
	if err != nil {
		t.Fatalf("KeyAt again: %v", err)
	}
	if string(first) != string(again) {
		t.Fatal("a second read made a new key, so everything sealed with the first is lost")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("the key is %v, want it readable only by its owner", perm)
	}
}

func TestARubbishKeyIsRefused(t *testing.T) {
	if _, err := secrets.Seal([]byte("too short"), "value"); err == nil {
		t.Fatal("a key of the wrong length was accepted")
	}
	path := filepath.Join(t.TempDir(), "secrets.key")
	if err := os.WriteFile(path, []byte("not hexadecimal at all\n"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := secrets.KeyAt(path); err == nil {
		t.Fatal("a key file that is not a key was accepted")
	}
}
