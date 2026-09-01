package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/console"
)

// The word is still in somebody's fingers, in their notes and in the panel they had open yesterday.
// It fails by name and says where its two pieces of data went, rather than becoming an unknown
// command, which reads as the tool being broken.
func TestTheHeaderWordIsRefusedAndNamesTheFooter(t *testing.T) {
	err := run(context.Background(), testClient(t), []string{"header"}, io.Discard, "")
	if err == nil {
		t.Fatal("krewe header was accepted, and there is no header any more")
	}
	said := err.Error()
	for _, want := range []string{"footer", "krewe room"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q: %s", want, said)
		}
	}
}

// The address survives the process that wrote it, which is the whole point: the console the panel
// opens tomorrow is a different process from the one open now.
func TestTheAddressIsWrittenDownAndReadBack(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())

	where := console.Place{
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
	if err := savePlace(where); err != nil {
		t.Fatalf("savePlace: %v", err)
	}

	read, err := loadPlace()
	if err != nil {
		t.Fatalf("loadPlace: %v", err)
	}
	if !read.Same(where) {
		t.Fatalf("the address came back as %+v, want %+v", read, where)
	}
}

// A console that has never been opened has nothing to read, and that is not an error screen: it opens
// at the top, which is where it opened before it remembered anything.
func TestNothingWrittenDownIsNowhereRatherThanAFailure(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())

	read, err := loadPlace()
	if err == nil && !read.Empty() {
		t.Fatalf("a system that never wrote a place read back %+v", read)
	}
	if !read.Empty() {
		t.Fatalf("an unreadable place is not empty: %+v", read)
	}
}

// A file somebody edited, or one written by a build that kept a different shape, is nowhere rather
// than a console that refuses to open.
func TestAnUnreadablePlaceOpensAtTheTop(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())

	path, err := placeFile()
	if err != nil {
		t.Fatalf("placeFile: %v", err)
	}
	if err := writeFileFor(path, "this is not an address"); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	read, err := loadPlace()
	if err == nil {
		t.Fatal("a file that is not an address was read as one")
	}
	if !read.Empty() {
		t.Fatalf("an unreadable place came back as %+v, want nowhere", read)
	}
}
