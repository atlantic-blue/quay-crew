package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrewTokenPrefersTheEnvironment(t *testing.T) {
	getenv := env(map[string]string{"QC_TOKEN": " from-the-environment \n"})
	read := func(string) ([]byte, error) {
		t.Fatal("the file was read when the environment had the token")
		return nil, nil
	}

	if got := crewToken(getenv, read); got != "from-the-environment" {
		t.Fatalf("crewToken = %q, want the trimmed environment value", got)
	}
}

func TestCrewTokenReadsTheFileWhereTheOperatorMovedTheData(t *testing.T) {
	getenv := env(map[string]string{"QC_DATA_HOST": "/somewhere/else"})
	read := func(path string) ([]byte, error) {
		if path != filepath.Join("/somewhere/else", "crew.token") {
			return nil, fmt.Errorf("read %s, want the moved data directory", path)
		}
		return []byte("from-the-moved-file\n"), nil
	}

	if got := crewToken(getenv, read); got != "from-the-moved-file" {
		t.Fatalf("crewToken = %q, want the file under QC_DATA_HOST", got)
	}
}

func TestCrewTokenFallsBackToTheCrewsDataDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnv, home)
	read := func(path string) ([]byte, error) {
		want := filepath.Join(home, "data", "crew.token")
		if path != want {
			return nil, fmt.Errorf("read %s, want %s", path, want)
		}
		return []byte("from-the-default-file"), nil
	}

	if got := crewToken(env(nil), read); got != "from-the-default-file" {
		t.Fatalf("crewToken = %q, want the file under the crew's data directory", got)
	}
}

func TestCrewTokenIsNothingWhenThereIsNothing(t *testing.T) {
	read := func(path string) ([]byte, error) { return nil, os.ErrNotExist }
	if got := crewToken(env(nil), read); got != "" {
		t.Fatalf("crewToken = %q, want nothing: the crew's refusal is what says what to set", got)
	}
	// A file of whitespace is nothing too, not a token of spaces.
	read = func(string) ([]byte, error) { return []byte(" \n"), nil }
	if got := crewToken(env(nil), read); got != "" {
		t.Fatalf("crewToken = %q, want nothing for a whitespace file", got)
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
