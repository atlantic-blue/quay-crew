package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/atlantic-blue/quay-krewe/internal/console"
)

// placeFile is where the console writes the address it is standing at, so the next run opens there.
//
// One small file under the data directory the tool already owns, rather than a setting in the system:
// where one operator was looking is not something the crew needs to know, and a console that cannot
// write it still works.
func placeFile() (string, error) {
	home, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "console-place"), nil
}

// loadPlace reads back where the console was. Anything unreadable is nowhere, which opens at the top.
func loadPlace() (console.Place, error) {
	path, err := placeFile()
	if err != nil {
		return console.Place{}, err
	}
	said, err := os.ReadFile(path) //nolint:gosec // a path this process built, under its own data directory
	if err != nil {
		return console.Place{}, err
	}
	var where console.Place
	if err := json.Unmarshal(said, &where); err != nil {
		return console.Place{}, err
	}
	return where, nil
}

// savePlace writes down where the console is standing.
func savePlace(where console.Place) error {
	path, err := placeFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	written, err := json.Marshal(where)
	if err != nil {
		return err
	}
	return os.WriteFile(path, written, 0o644) //nolint:gosec // an address, not a secret
}

// remembering is the store the console keeps its address in.
func remembering() console.PlaceStore {
	return console.PlaceStore{Load: loadPlace, Save: savePlace}
}

// writeFileFor puts a body at a path, making the directory above it. It exists so a test can write a
// place file the loader will refuse, without repeating what the loader knows about where that is.
func writeFileFor(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644) //nolint:gosec // a test fixture, not a secret
}
