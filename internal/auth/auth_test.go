package auth_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/auth"
)

func TestTokenAtMintsOnceAndReadsItBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")

	minted, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if len(minted) != 64 {
		t.Fatalf("a minted token is %d characters, want 64", len(minted))
	}

	read, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if read != minted {
		t.Fatalf("read back %q, want the minted %q", read, minted)
	}
}

func TestTokenAtWritesForTheOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if _, err := auth.TokenAt(path); err != nil {
		t.Fatalf("minting: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("the token file is %v, want 0600", got)
	}
}

func TestTokenAtTrimsWhatAnEditorLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if err := os.WriteFile(path, []byte("  a-token-somebody-set-by-hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := auth.TokenAt(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if token != "a-token-somebody-set-by-hand" {
		t.Fatalf("read %q, want the trimmed token", token)
	}
}

func TestTokenAtRefusesAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crew.token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.TokenAt(path); err == nil {
		t.Fatal("an empty token file was accepted, want a refusal: an empty token would let an empty call in")
	}
}
