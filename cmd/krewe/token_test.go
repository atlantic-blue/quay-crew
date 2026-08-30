package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemTokenPrefersTheEnvironment(t *testing.T) {
	getenv := env(map[string]string{"QC_TOKEN": " from-the-environment \n"})
	read := func(string) ([]byte, error) {
		t.Fatal("the file was read when the environment had the token")
		return nil, nil
	}

	if got := systemToken(getenv, read); got != "from-the-environment" {
		t.Fatalf("systemToken = %q, want the trimmed environment value", got)
	}
}

func TestSystemTokenReadsTheFileWhereTheOperatorMovedTheData(t *testing.T) {
	getenv := env(map[string]string{"QC_DATA_HOST": "/somewhere/else"})
	read := func(path string) ([]byte, error) {
		if path != filepath.Join("/somewhere/else", "system.token") {
			return nil, fmt.Errorf("read %s, want the moved data directory", path)
		}
		return []byte("from-the-moved-file\n"), nil
	}

	if got := systemToken(getenv, read); got != "from-the-moved-file" {
		t.Fatalf("systemToken = %q, want the file under QC_DATA_HOST", got)
	}
}

func TestSystemTokenFallsBackToTheSystemsDataDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnv, home)
	read := func(path string) ([]byte, error) {
		want := filepath.Join(home, "data", "system.token")
		if path != want {
			return nil, fmt.Errorf("read %s, want %s", path, want)
		}
		return []byte("from-the-default-file"), nil
	}

	if got := systemToken(env(nil), read); got != "from-the-default-file" {
		t.Fatalf("systemToken = %q, want the file under the system's data directory", got)
	}
}

func TestSystemTokenIsNothingWhenThereIsNothing(t *testing.T) {
	read := func(path string) ([]byte, error) { return nil, os.ErrNotExist }
	if got := systemToken(env(nil), read); got != "" {
		t.Fatalf("systemToken = %q, want nothing: the system's refusal is what says what to set", got)
	}
	// A file of whitespace is nothing too, not a token of spaces.
	read = func(string) ([]byte, error) { return []byte(" \n"), nil }
	if got := systemToken(env(nil), read); got != "" {
		t.Fatalf("systemToken = %q, want nothing for a whitespace file", got)
	}
}

// env is a getenv over a map, so a test never reads this machine's real environment.
func env(values map[string]string) func(string) string {
	return func(name string) string {
		if strings.TrimSpace(name) == "" {
			return ""
		}
		return values[name]
	}
}
