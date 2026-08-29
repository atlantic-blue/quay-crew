package role

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// notOurs is a name a role this build ships, and the writing about one, must not carry: the name
// itself, the prefix it put on the front of its own agents, and the prefix its own commands took.
//
// The roles are quay's own. A brief carrying another product's name sends a session looking for a
// file, a command or an agent that is not here, and a reader of an open repository takes the name as
// a dependency this build has.
var notOurs = regexp.MustCompile(`(?i)greenlight|\bgl-|/gl:`)

// swept is where the roles live and where the writing about them lives, from this package's
// directory. Directories rather than a list of files, so a role added tomorrow and a document
// written tomorrow are both held to this without anybody remembering to add them.
//
// Two things are deliberately outside the sweep. CHANGELOG.md, because an entry says what shipped on
// a day and a changelog edited to match today is not a record of anything. And the Go under
// features/, because a step file that enforces this rule has to hold the name to match it, as this
// file does; the scenarios a person reads are .feature files and every one of them is swept.
var swept = []struct {
	dir string
	// only is the file extension swept under this directory, or empty for every file.
	only string
	// least is a floor on the files opened, not a count. A sweep that opens nothing finds nothing,
	// and finding nothing is exactly what a clean tree looks like.
	least int
}{
	{dir: "../../roles", least: 30},
	{dir: "../../flows", only: ".yaml", least: 2},
	{dir: "../../docs", least: 10},
	{dir: "../../features", only: ".feature", least: 30},
}

// naming is one line carrying a name this repository's own material may not carry.
type naming struct {
	file string
	line int
	text string
}

// namesAnotherProduct reads every file under root, or every file with the extension given, and
// reports each line naming a product that is not quay. It returns the count of files it opened as
// well, because no findings over no files reads exactly like a clean sweep.
func namesAnotherProduct(root, only string) ([]naming, int, error) {
	var found []naming
	files := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if only != "" && filepath.Ext(path) != only {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for at, line := range strings.Split(string(body), "\n") {
			if notOurs.MatchString(line) {
				found = append(found, naming{file: path, line: at + 1, text: strings.TrimSpace(line)})
			}
		}
		return nil
	})
	return found, files, err
}

// The guard. No role this build ships, and no document or scenario written about one, carries the
// name of a product that is not quay.
func TestNothingWeShipNamesAnotherProduct(t *testing.T) {
	for _, root := range swept {
		found, opened, err := namesAnotherProduct(root.dir, root.only)
		if err != nil {
			t.Fatalf("reading %s: %v", root.dir, err)
		}
		for _, one := range found {
			t.Errorf("%s:%d names a product that is not quay: %s", one.file, one.line, one.text)
		}
		// The count is reported so a sweep that opened four files cannot read as one that opened
		// them all.
		t.Logf("read %d files under %s", opened, root.dir)
		if opened < root.least {
			t.Errorf("the sweep opened %d files under %s and there are at least %d, so it swept a tree that is not there",
				opened, root.dir, root.least)
		}
	}
}

// The check that the check works. A pattern that stopped matching reports a clean tree exactly as a
// clean tree does, so the guard above is worth nothing until this has watched it catch something.
func TestTheGuardCatchesAProductNameAnAgentPrefixAndACommand(t *testing.T) {
	for _, one := range []struct {
		what string
		line string
	}{
		{"the name in prose", "these briefs are greenlight's, and this crew has none of them"},
		{"the name capitalised", "You are the Greenlight architect."},
		{"the name in a path", "Write `.greenlight/ASSESS.md` following this structure."},
		{"an agent by its prefix", "Tests are written by gl-test-writer."},
		{"a command", "You are spawned by /gl:slice."},
	} {
		t.Run(one.what, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ROLE.md"), []byte("clean\n"+one.line+"\n"), 0o644); err != nil {
				t.Fatalf("planting the file: %v", err)
			}
			found, files, err := namesAnotherProduct(dir, "")
			if err != nil {
				t.Fatalf("walking the planted tree: %v", err)
			}
			if files != 1 {
				t.Fatalf("the sweep opened %d files and one was planted", files)
			}
			if len(found) != 1 {
				t.Fatalf("the guard found %d namings in %q, want 1", len(found), one.line)
			}
			if found[0].line != 2 {
				t.Errorf("the guard points at line %d and the naming is on line 2", found[0].line)
			}
		})
	}
}

// The sad paths. A guard that cannot tell an empty tree from a clean one, or that fires on an
// ordinary word, is a guard nobody can keep.
func TestTheGuardOnAnEmptyTreeAndOnWordsARoleMayCarry(t *testing.T) {
	found, files, err := namesAnotherProduct(t.TempDir(), "")
	if err != nil {
		t.Fatalf("walking an empty tree: %v", err)
	}
	if len(found) != 0 || files != 0 {
		t.Fatalf("an empty tree gave %d namings over %d files", len(found), files)
	}

	dir := t.TempDir()
	// Words a brief legitimately carries. "Greenfield" opens with five letters of the name and is not
	// it, and "single-file" ends in the two letters the agent prefix starts with.
	clean := "Otherwise: 'Greenfield project'\na single-file module\nthe wrangling-heavy path\n"
	if err := os.WriteFile(filepath.Join(dir, "ROLE.md"), []byte(clean), 0o644); err != nil {
		t.Fatalf("planting the file: %v", err)
	}
	found, files, err = namesAnotherProduct(dir, "")
	if err != nil {
		t.Fatalf("walking the planted tree: %v", err)
	}
	if files != 1 {
		t.Fatalf("the sweep opened %d files and one was planted", files)
	}
	if len(found) != 0 {
		t.Fatalf("the guard fired on words a brief may carry: %+v", found)
	}
}

// The extension filter reads the files it says it reads and no others, because a filter that matched
// nothing would sweep every scenario file out of the guard without saying so.
func TestTheExtensionFilterSweepsOnlyWhatItNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "roles.feature"), []byte("ported from greenlight\n"), 0o644); err != nil {
		t.Fatalf("planting the scenario: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "steps_test.go"), []byte("ported from greenlight\n"), 0o644); err != nil {
		t.Fatalf("planting the step file: %v", err)
	}
	found, files, err := namesAnotherProduct(dir, ".feature")
	if err != nil {
		t.Fatalf("walking the planted tree: %v", err)
	}
	if files != 1 || len(found) != 1 {
		t.Fatalf("the filter opened %d files and found %d namings, want one of each", files, len(found))
	}
	if filepath.Ext(found[0].file) != ".feature" {
		t.Errorf("the filter read %s and it was told .feature only", found[0].file)
	}
}

// A directory that is not there is a failure, never a clean sweep. This is the shape that would let
// the guard pass forever after somebody moved roles/.
func TestTheGuardRefusesADirectoryThatIsNotThere(t *testing.T) {
	if _, _, err := namesAnotherProduct(filepath.Join(t.TempDir(), "nowhere"), ""); err == nil {
		t.Fatal("walking a directory that is not there reported no error, so a moved roles/ would sweep clean")
	}
}
