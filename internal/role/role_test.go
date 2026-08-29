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
			"summary: writes the tests for a job, from the job alone",
			"model: opus",
			"receives:",
			"  - job",
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
	if got := strings.Join(loaded.Receives, ","); got != "context,job" {
		t.Errorf("it receives %q, want context,job", got)
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
		"  - job",
		"  - the whole repository",
	)))
	if err == nil {
		t.Fatal("a role receiving material the crew does not hand out was accepted")
	}
	for _, want := range []string{"the whole repository", "job", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A session with no job to do is not a task, so the one nonsense list is refused with a sentence
// saying what to add.
func TestARoleThatDoesNotReceiveTheJobIsRefused(t *testing.T) {
	_, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: writes the tests",
		"model: opus",
		"receives:",
		"  - context",
	)))
	if err == nil {
		t.Fatal("a role that receives no job was accepted")
	}
	if !strings.Contains(err.Error(), "job") {
		t.Errorf("the refusal does not say to add job: %v", err)
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
		"  - job",
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
		"  - job",
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
		"  - job",
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
		"  - job",
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
	// A brief the size of the ones this build ships is not over the limit, which is the point of the
	// number.
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
	summary := "summary: writes the tests for a job, from the job alone"
	changed := []struct {
		what  string
		files []File
	}{
		{"the brief", replace(good(), BriefFile, "Write the code instead.")},
		{"the model", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", summary,
			"model: haiku", "receives:", "  - job", "  - context"))},
		{"what it receives", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", summary,
			"model: opus", "receives:", "  - job", "  - skills"))},
		{"the summary", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 1", "summary: something else entirely",
			"model: opus", "receives:", "  - job", "  - context"))},
		{"the version", replace(good(), ManifestFile, manifestOf(
			"name: test-writer", "version: 2", summary,
			"model: opus", "receives:", "  - job", "  - context"))},
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
	if !loaded.Gets(MaterialJob) || loaded.Gets(MaterialSkills) {
		t.Errorf("what it receives read as %v", loaded.Receives)
	}
}

// A role says what a session running as it may do, as well as what it may see. Both boundaries live
// in one file, so a reader holds one answer rather than two.

// mayDo is the valid role with a may list on it.
func mayDo(verbs ...string) []File {
	lines := []string{
		"name: test-writer",
		"version: 1",
		"summary: writes the tests for a job, from the job alone",
		"model: opus",
		"receives:",
		"  - job",
	}
	if len(verbs) > 0 {
		lines = append(lines, "may:")
		for _, verb := range verbs {
			lines = append(lines, "  - "+verb)
		}
	}
	return replace(good(), ManifestFile, manifestOf(lines...))
}

func TestARoleDeclaresTheVerbsItMayCall(t *testing.T) {
	loaded, err := FromFiles(mayDo(VerbJobCreate, VerbJobRead))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}

	for _, verb := range []string{VerbJobCreate, VerbJobRead} {
		if !loaded.May(verb) {
			t.Errorf("the role may not %s, and it declared it", verb)
		}
	}
	for _, verb := range []string{VerbJobStop, VerbJobAnswer} {
		if loaded.May(verb) {
			t.Errorf("the role may %s, and it never declared it", verb)
		}
	}
}

// A role that declares nothing may call nothing. Default deny, which is what every role imported
// before this existed becomes.
func TestARoleThatDeclaresNoVerbsMayCallNothing(t *testing.T) {
	loaded, err := FromFiles(good())
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}

	for _, verb := range Verbs {
		if loaded.May(verb) {
			t.Errorf("a role that declared nothing may %s", verb)
		}
	}
}

// A word the crew does not know is a boundary that quietly means nothing, so it is refused while
// somebody is looking at the file.
func TestAVerbTheCrewDoesNotKnowIsRefusedByName(t *testing.T) {
	_, err := FromFiles(mayDo(VerbJobCreate, "workspace.create"))

	if err == nil {
		t.Fatal("a verb the crew does not know was accepted")
	}
	if !strings.Contains(err.Error(), "workspace.create") {
		t.Errorf("the refusal says %q, want it to name the verb", err)
	}
	for _, verb := range Verbs {
		if !strings.Contains(err.Error(), verb) {
			t.Errorf("the refusal says %q, want it to offer %q", err, verb)
		}
	}
}

// Four verbs and no more: a verb nobody uses is a boundary that means nothing.
func TestTheVerbsAreTheFourJobVerbs(t *testing.T) {
	if got := strings.Join(Verbs, ","); got != "job.create,job.read,job.answer,job.stop" {
		t.Fatalf("the verbs are %q, want the four job verbs and no more", got)
	}
}

// The same version carrying different verbs is a different role, or a workspace pinned to a version
// would find its boundary changed underneath it.
func TestTheVerbsArePartOfWhatAVersionIs(t *testing.T) {
	narrow, err := FromFiles(mayDo(VerbJobRead))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	wide, err := FromFiles(mayDo(VerbJobRead, VerbJobCreate))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}

	if narrow.Fingerprint() == wide.Fingerprint() {
		t.Fatal("two roles with different verbs have the same fingerprint, so one could replace the other")
	}
}

// The verbs come back sorted and without repeats, so a listing and a fingerprint do not depend on
// the order somebody happened to type them in.
func TestTheVerbsAreTidiedTheWayTheMaterialIs(t *testing.T) {
	loaded, err := FromFiles(mayDo(VerbJobRead, VerbJobCreate, VerbJobRead))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}

	if got := strings.Join(loaded.May_, ","); got != VerbJobCreate+","+VerbJobRead {
		t.Fatalf("the verbs read back as %q, want them sorted and once each", got)
	}
}

// write puts a role's files in a directory of its own under root, so a test can build a set of them.
func write(t *testing.T, root, name string, files []File) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Path), file.Body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestASetOfRolesReadsBackSortedByName(t *testing.T) {
	root := t.TempDir()
	write(t, root, "test-writer", good())
	write(t, root, "architect", replace(good(), ManifestFile, manifestOf(
		"name: architect", "version: 1", "summary: writes the contracts",
		"model: opus", "receives:", "  - job",
	)))

	roles, err := All(root)
	if err != nil {
		t.Fatalf("reading a directory of roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("read %d roles and wrote 2", len(roles))
	}
	if roles[0].Name != "architect" || roles[1].Name != "test-writer" {
		t.Errorf("the set is not sorted by name: %s, %s", roles[0].Name, roles[1].Name)
	}
	if roles[0].Dir != filepath.Join(root, "architect") {
		t.Errorf("a role does not carry where it was read from: %q", roles[0].Dir)
	}
}

// Finding nothing to do is indistinguishable from doing it all correctly, so a directory holding no
// roles is an error rather than an empty set. A check on the roles a build ships would otherwise
// report success against a directory that lost them.
func TestADirectoryHoldingNoRolesIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := All(root); err == nil {
		t.Fatal("a directory holding no roles was read as a set of no roles")
	} else if !strings.Contains(err.Error(), "no roles") {
		t.Errorf("the refusal does not say the directory holds no roles: %v", err)
	}
}

// A directory holding only files, or only directories that are not roles, is the same emptiness
// wearing a fuller shape.
func TestADirectoryOfThingsThatAreNotRolesIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("roles go here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := All(root); err == nil {
		t.Fatal("a directory holding no role manifest was read as a set of roles")
	}
}

// One bad role fails the set. Skipping it would ship a build whose roles/ holds twelve directories
// and imports eleven, and the eleven would look like all of them.
func TestOneRoleThatWillNotLoadFailsTheWholeSet(t *testing.T) {
	root := t.TempDir()
	write(t, root, "test-writer", good())
	write(t, root, "architect", replace(good(), ManifestFile, manifestOf(
		"name: architect", "version: 1", "summary: writes the contracts",
		"model: opus", "receives:", "  - the whole repository",
	)))

	if _, err := All(root); err == nil {
		t.Fatal("a set holding a role the crew would refuse was accepted")
	} else if !strings.Contains(err.Error(), "the whole repository") {
		t.Errorf("the refusal does not name what was wrong: %v", err)
	}
}

func TestADirectoryThatIsNotThereIsRefused(t *testing.T) {
	if _, err := All(filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Fatal("a directory that does not exist was read as a set of roles")
	}
}

// Every word the crew retired is refused at import, by name, with what to write instead.
//
// The guard is over the table rather than a case per word, so the next rename is covered the moment
// its entry is added. A word that is merely unknown says only that the crew does not have it, which
// sends the author looking; a word quietly accepted is worse than both, because the boundary then
// means nothing and reads exactly like one that holds.
func TestEveryRetiredWordIsRefusedByNameAndSaysWhatToWrite(t *testing.T) {
	if len(Retired) == 0 {
		t.Fatal("the retired table is empty, so this test proves nothing")
	}

	for was, becomes := range Retired {
		// A material is a plain word and a verb is dotted, which is what lets one table serve both.
		manifest := "name: backlog-clearer\nversion: 1\nsummary: clears the backlog\nmodel: opus\n"
		if strings.Contains(was, ".") {
			manifest += "receives:\n  - job\nmay:\n  - " + was + "\n"
		} else {
			manifest += "receives:\n  - " + was + "\n"
		}

		dir := filepath.Join(t.TempDir(), "backlog-clearer")
		write(t, filepath.Dir(dir), "backlog-clearer", []File{
			{Path: ManifestFile, Body: []byte(manifest)},
			{Path: BriefFile, Body: []byte("Read the open pull requests.")},
		})

		_, err := One(dir)
		if err == nil {
			t.Errorf("a role carrying %q was imported, so the word means nothing and looks exactly "+
				"like one that holds", was)
			continue
		}
		// It names the word, what to write instead, and that the word was renamed. The last part is
		// what makes this a refusal rather than the allow list saying no: "work is not material the
		// crew hands out, it is one of job, context, skills" contains both words already, and it sends
		// the author to guess which of the three their manifest meant.
		for _, want := range []string{was, becomes, "is called"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("a role carrying %q is refused with %q, want it to say %q", was, err, want)
			}
		}
	}
}

// Nothing the crew retired is a word it still takes, which is how a rename reintroduces the old
// spelling: the entry is added to the retired table and the allow list is never cleaned out, so the
// word is refused and accepted by two rules that disagree.
func TestNoRetiredWordIsStillAWordTheCrewTakes(t *testing.T) {
	for was := range Retired {
		if known(was) {
			t.Errorf("%q is retired and still material the crew hands out", was)
		}
		if knownVerb(was) {
			t.Errorf("%q is retired and still a verb the crew grants", was)
		}
	}
	// And every replacement is a word the crew does take, so the advice can be acted on.
	for was, becomes := range Retired {
		if !known(becomes) && !knownVerb(becomes) {
			t.Errorf("%q is retired in favour of %q, which the crew does not take either", was, becomes)
		}
	}
}
