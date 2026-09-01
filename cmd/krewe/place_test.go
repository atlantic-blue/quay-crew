package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/console"
)

// aPlace is where the console was standing, three levels down, which is what has to survive being
// written to a file and read back.
func aPlace() console.Place {
	return console.Place{
		View:   "jobs",
		Parent: "2222222222222222bbbbbbbb",
		Levels: []console.Level{
			{Resource: "workspaces", Row: "1111111111111111aaaaaaaa", Into: "acme", Typed: "acme"},
			{
				Resource: "projects", Parent: "1111111111111111aaaaaaaa",
				Row: "2222222222222222bbbbbbbb", Into: "house-bills", Typed: "house-bills",
			},
		},
	}
}

func TestWhereTheConsoleWasSurvivesBeingWrittenDownAndReadBack(t *testing.T) {
	t.Setenv("KREWE_HOME", t.TempDir())

	if err := savePlace(aPlace()); err != nil {
		t.Fatalf("writing the place down: %v", err)
	}
	read, err := loadPlace()
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}

	if read.View != "jobs" || read.Parent != "2222222222222222bbbbbbbb" {
		t.Fatalf("what came back is %q scoped to %q", read.View, read.Parent)
	}
	if len(read.Levels) != 2 {
		t.Fatalf("what came back carries %d levels, want the two drilled through", len(read.Levels))
	}
	// Both halves of a level, because the breadcrumb reads one and the position line reads the other,
	// and a file that kept only one would quietly lose whichever it dropped.
	if read.Levels[1].Into != "house-bills" || read.Levels[1].Typed != "house-bills" {
		t.Fatalf("the second level came back as %+v", read.Levels[1])
	}
	if read.Levels[1].Row != "2222222222222222bbbbbbbb" {
		t.Fatalf("the second level names row %q, want the whole identifier a resume checks for",
			read.Levels[1].Row)
	}
}

// It goes beside the panel's own view file, in the system's directory, rather than into the checkout.
func TestThePlaceIsKeptInTheSystemsOwnDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KREWE_HOME", home)

	if err := savePlace(aPlace()); err != nil {
		t.Fatalf("writing the place down: %v", err)
	}
	written := filepath.Join(home, "console-place")
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("nothing was written to %s: %v", written, err)
	}
}

// Nothing written yet is nowhere, which is what a console that has never been opened resumes to.
func TestAHomeWithNoPlaceInItResumesToNowhere(t *testing.T) {
	t.Setenv("KREWE_HOME", t.TempDir())

	if _, err := loadPlace(); err == nil {
		t.Fatal("a home with no place in it read one, so the console would resume to something invented")
	}
	if where := consoleResume(); !where.Empty() {
		t.Fatalf("with nothing written the console resumes to %+v, want nowhere", where)
	}
}

// A file from a build that wrote it differently is nowhere too. It is read from a directory an
// upgrade never rewrites, so this is the ordinary case after one.
func TestAPlaceFileThisBuildCannotReadResumesToNowhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KREWE_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "console-place"), []byte("jobs\nacme\n"), 0o644); err != nil {
		t.Fatalf("writing a file this build cannot read: %v", err)
	}

	if where := consoleResume(); !where.Empty() {
		t.Fatalf("an unreadable place resumed to %+v, want nowhere", where)
	}
}

// consoleResume is what the console does with the store on the way up, so a case reads the same path
// the tool takes rather than a description of it.
func consoleResume() console.Place {
	store := thePlaceStore()
	where, err := store.Load()
	if err != nil {
		return console.Place{}
	}
	return where
}
