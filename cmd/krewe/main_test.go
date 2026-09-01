package main

import (
	"os"
	"testing"
)

// TestMain points the system's directory at a temporary one for every case in this package.
//
// The tool writes the address you are working in, and the panel's view, into that directory. A test
// that forgets to isolate itself would write those into the operator's own system and move them
// somewhere they did not ask to be. Two cases isolated themselves through XDG_CONFIG_HOME, which
// stopped isolating anything the moment the directory stopped following it, and the failure would
// have been silent: the tests pass either way, and the operator finds out later.
//
// A case that is about the directory itself still sets KREWE_HOME to whatever it needs.
func TestMain(m *testing.M) {
	temporary, err := os.MkdirTemp("", "krewe-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv(HomeEnv, temporary); err != nil {
		panic(err)
	}

	code := m.Run()
	_ = os.RemoveAll(temporary)
	os.Exit(code)
}
