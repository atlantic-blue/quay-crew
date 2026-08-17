package role

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a valid role, which each test then breaks in one way.
func manifestOf(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func good() []File {
	return []File{
		{Path: ManifestFile, Body: []byte(manifestOf(
			"name: test-writer",
			"version: 1",
			"summary: writes the tests for a piece of work, from the work alone",
			"model: opus",
			"receives:",
			"  - work",
			"  - context",
		))},
		{Path: BriefFile, Body: []byte("Write the tests. Do not write the code.")},
	}
}

func without(files []File, path string) []File {
	out := make([]File, 0, len(files))
	for _, file := range files {
		if file.Path != path {
			out = append(out, file)
		}
	}
	return out
}

func replace(files []File, path, body string) []File {
	out := make([]File, len(files))
	copy(out, files)
	for at := range out {
		if out[at].Path == path {
			out[at].Body = []byte(body)
		}
	}
	return out
}

func TestARoleIsReadFromItsManifestAndItsBrief(t *testing.T) {
	loaded, err := FromFiles(good())
	if err != nil {
		t.Fatalf("a valid role was refused: %v", err)
	}
	if loaded.Name != "test-writer" || loaded.Version != 1 {
		t.Errorf("it read as %q version %d", loaded.Name, loaded.Version)
	}
	if loaded.Model != "opus" {
		t.Errorf("it runs on %q, want opus", loaded.Model)
	}
	if loaded.Brief != "Write the tests. Do not write the code." {
		t.Errorf("the brief read as %q", loaded.Brief)
	}
	// Sorted, so what a role receives does not depend on the order somebody typed it in.
	if got := strings.Join(loaded.Receives, ","); got != "context,work" {
		t.Errorf("it receives %q, want context,work", got)
	}
}

// The boundary is the whole point of a role, so a role that receives something the crew does not
// hand out has to be refused at import. Accepted, it would read as a boundary and hold nothing.
func TestAMaterialTheCrewDoesNotHandOutIsRefusedByName(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus",
		"receives:",
		"  - work",
		"  - the whole repository",
	)))
	if err == nil {
		t.Fatal("a role receiving material the crew does not hand out was accepted")
	}
	for _, want := range []string{"the whole repository", "work", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A session with no work to do is not a task, so the one nonsense list is refused with a sentence
// saying what to add.
func TestARoleThatDoesNotReceiveTheWorkIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus",
		"receives:",
		"  - context",
	)))
	if err == nil {
		t.Fatal("a role that receives no work was accepted")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("the refusal does not say to add work: %v", err)
	}
}

func TestARoleThatDeclaresNoBoundaryIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus",
	)))
	if err == nil {
		t.Fatal("a role that says nothing about what it receives was accepted")
	}
	if !strings.Contains(err.Error(), "boundary") {
		t.Errorf("the refusal does not say a role is its boundary: %v", err)
	}
}

// What a role costs is part of what it is, so the crew will not guess it.
func TestARoleWithNoModelIsRefusedAndSaysWhatToWrite(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"receives:",
		"  - work",
	)))
	if err == nil {
		t.Fatal("a role naming no model was accepted")
	}
	for _, want := range []string{"opus", "claude-opus-5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not show what a model name looks like: %v", err)
		}
	}
}

func TestAModelThatIsAShellFragmentIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus; rm -rf /",
		"receives:",
		"  - work",
	)))
	if err == nil {
		t.Fatal("a model name carrying a shell fragment was accepted")
	}
}

func TestTheManifestAndTheBriefAreBothRequired(t *testing.T) {
	if _, err := FromFiles(without(good(), ManifestFile)); err == nil {
		t.Error("a role with no manifest was accepted")
	}
	if _, err := FromFiles(without(good(), BriefFile)); err == nil {
		t.Error("a role with no brief was accepted")
	}
}

// A field the crew does not know is refused by name rather than ignored. Ignored, it looks
// configured and does nothing, which sends whoever wrote it looking somewhere else entirely.
func TestAFieldTheCrewDoesNotKnowIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus",
		"tools: [bash]",
		"receives:",
		"  - work",
	)))
	if err == nil {
		t.Fatal("a manifest carrying a field the crew does not know was accepted")
	}
	if !strings.Contains(err.Error(), "tools") {
		t.Errorf("the refusal does not name the field: %v", err)
	}
}

func TestAVersionIsRequiredSoASessionCanBePinned(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"summary: writes the tests",
		"model: opus",
		"receives:",
		"  - work",
	)))
	if err == nil {
		t.Fatal("a role with no version was accepted")
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Errorf("the refusal does not say why a version is needed: %v", err)
	}
}

func TestABriefLongerThanFourPagesIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), BriefFile, strings.Repeat("a", BriefLimit+1)))
	if err == nil {
		t.Fatal("a brief over the limit was accepted")
	}
	// A brief the size of the ones this shape was taken from is not over the limit, which is the
	// point of the number.
	if _, err := FromFiles(replace(good(), BriefFile, strings.Repeat("a", 12000))); err != nil {
		t.Errorf("a brief of twelve thousand bytes was refused: %v", err)
	}
}

// Two revisions differing anywhere have different fingerprints, which is what makes importing the
// same version twice refusable rather than a silent overwrite.
func TestTheFingerprintCoversEveryDeclaredField(t *testing.T) {
	base, err := FromFiles(good())
	if err != nil {
		t.Fatalf("a valid role was refused: %v", err)
	}
	same, err := FromFiles(good())
	if err != nil {
		t.Fatalf("a valid role was refused: %v", err)
	}
	if base.Fingerprint() != same.Fingerprint() {
		t.Error("the same role read twice fingerprints differently")
	}

	// Each case changes exactly one field and leaves every other one as good() wrote it. A case that
	// moved two would pass against a fingerprint that had stopped covering either.
	summary := "summary: writes the tests for a piece of work, from the work alone"
	changed := []struct {
		what  string
		files []File
	}{
		{"the brief", replace(good(), BriefFile, "Write the code instead.")},
		{"the model", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", summary,
			"model: haiku", "receives:", "  - work", "  - context"))},
		{"what it receives", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", summary,
			"model: opus", "receives:", "  - work", "  - skills"))},
		{"the summary", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", "summary: something else entirely",
			"model: opus", "receives:", "  - work", "  - context"))},
		{"the version", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 2", summary,
			"model: opus", "receives:", "  - work", "  - context"))},
	}
	for _, one := range changed {
		altered, err := FromFiles(one.files)
		if err != nil {
			t.Fatalf("%s: a valid role was refused: %v", one.what, err)
		}
		if altered.Fingerprint() == base.Fingerprint() {
			t.Errorf("changing %s did not change the fingerprint", one.what)
		}
	}
}

// A role read from a directory is named by that directory, so the same role cannot arrive under two
// names as soon as anybody attaches it.
func TestARoleIsTheDirectoryItLivesIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reviewer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range good() {
		if err := os.WriteFile(filepath.Join(dir, file.Path), file.Body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := One(dir)
	if err == nil {
		t.Fatal("a role calling itself something other than its directory was accepted")
	}
	if !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "test-writer") {
		t.Errorf("the refusal does not name both: %v", err)
	}
}

func TestARoleReadsBackWholeFromItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range good() {
		if err := os.WriteFile(filepath.Join(dir, file.Path), file.Body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := One(dir)
	if err != nil {
		t.Fatalf("reading a role from its directory: %v", err)
	}
	if loaded.Name != "test-writer" || loaded.Dir != dir {
		t.Errorf("it read as %q in %q", loaded.Name, loaded.Dir)
	}
	if !loaded.Gets(MaterialWork) || loaded.Gets(MaterialSkills) {
		t.Errorf("what it receives read as %v", loaded.Receives)
	}
}
