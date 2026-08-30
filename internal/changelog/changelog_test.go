package changelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a fragment in a directory of the test's own, so nothing here reads the repository.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// TestTwoChangesLandInOneSection.
//
// The point of a fragment: two changes write two files, and a release reads both. Newest first,
// which is the order the changelog has always been in.
func TestTwoChangesLandInOneSection(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "455-a-console-view-of-jobs.md", "**A console view of jobs.** What a job is doing.")
	write(t, dir, "480-changelog-fragments.md", "**One file per change.** So two changes never collide.")

	fragments, err := Collect(dir)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(fragments) != 2 {
		t.Fatalf("collected %d fragments, want 2: %+v", len(fragments), fragments)
	}
	if fragments[0].Number != 480 || fragments[1].Number != 455 {
		t.Errorf("collected %d then %d, want the newest first", fragments[0].Number, fragments[1].Number)
	}

	section := Render("30 August 2026", fragments)
	want := "## 30 August 2026\n\n" +
		"- **One file per change.** So two changes never collide.\n\n" +
		"- **A console view of jobs.** What a job is doing.\n"
	if section != want {
		t.Errorf("assembled:\n%s\nwant:\n%s", section, want)
	}
}

// TestAnEntryIsOneBullet.
//
// An entry is prose with paragraphs in it, and CHANGELOG.md holds each one as a single bullet. So
// everything under the first line is indented to sit inside that bullet, and a blank line stays
// blank rather than becoming two spaces of nothing.
func TestAnEntryIsOneBullet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "480-fragments.md", "**A title.** The first paragraph.\nIts second line.\n\nA second paragraph.\n")

	section, err := Assemble(dir, "30 August 2026")
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	want := "## 30 August 2026\n\n" +
		"- **A title.** The first paragraph.\n" +
		"  Its second line.\n" +
		"\n" +
		"  A second paragraph.\n"
	if section != want {
		t.Errorf("assembled:\n%q\nwant:\n%q", section, want)
	}
}

// TestTheReadmeIsNotAnEntry.
//
// The convention is written down next to the fragments, and a release that carried it as a change
// would say the repository shipped its own instructions.
func TestTheReadmeIsNotAnEntry(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "README.md", "how to write one")
	write(t, dir, ".keep", "")
	write(t, dir, "480-fragments.md", "**A title.** What changed.")

	fragments, err := Collect(dir)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(fragments) != 1 || fragments[0].File != "480-fragments.md" {
		t.Fatalf("collected %+v, want the one fragment", fragments)
	}
}

// TestAFragmentNobodyCanFileIsRefused.
//
// Refused rather than skipped. A fragment quietly left out of the release is work somebody did that
// nobody reads, and the file it was in still looks like it counted.
func TestAFragmentNobodyCanFileIsRefused(t *testing.T) {
	for _, one := range []struct {
		name, file, body, says string
	}{
		{name: "no issue number", file: "changelog-fragments.md", body: "**A title.** What changed.", says: "is not a fragment name"},
		{name: "a name with spaces in it", file: "480 fragments.md", body: "**A title.** What changed.", says: "is not a fragment name"},
		{name: "nothing written in it", file: "480-fragments.md", body: "\n  \n", says: "is empty"},
	} {
		t.Run(one.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, one.file, one.body)

			_, err := Collect(dir)
			if err == nil {
				t.Fatalf("collected %s without complaining", one.file)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Errorf("said %q, want it to say %q and name the file", err, one.says)
			}
			if !strings.Contains(err.Error(), one.file) {
				t.Errorf("said %q, which does not name %s", err, one.file)
			}
		})
	}
}

// TestNothingToAssembleIsNotARelease.
//
// An empty section reads exactly like a section that assembled correctly, so it is refused. The
// same holds for a directory that is not there at all.
func TestNothingToAssembleIsNotARelease(t *testing.T) {
	dir := t.TempDir()
	if _, err := Assemble(dir, "30 August 2026"); err == nil {
		t.Error("assembled a release out of an empty directory")
	} else if !strings.Contains(err.Error(), "no fragments") {
		t.Errorf("said %q, want it to say there are no fragments", err)
	}

	if _, err := Assemble(filepath.Join(dir, "nowhere"), "30 August 2026"); err == nil {
		t.Error("assembled a release out of a directory that is not there")
	}
}
