package job_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// An operator reads the list of every length cap in this system and what each one became.
//
// A change that lifts the caps one at a time leaves nobody able to say which ones it lifted. The
// operator who accepts the work has to read the source to find out, and a cap nobody wrote down is a
// cap that still refuses text next month. So the change carries a list, the list covers every cap in
// the source, and each line says what happened to that cap.
//
// The list is prose in a committed markdown file, because that is the text the pull request body
// carries in this repository: a change writes its entry in changelog.d and the pull request says the
// same thing. These tests do not say which file it must be. They read every markdown file that is
// committed here, take the one that names the most caps, and hold that one to the requirement.
//
// A cap is found rather than listed by hand. A constant that this system compares against the length
// of something is a cap, whatever it is called, so the list cannot go stale when somebody adds one.
// The scan is wide on purpose. It also finds the constants that cap how many rather than how long,
// such as how many attempts a loop takes, because no scan can tell a count of words from a count of
// steps without reading the types. A wide list costs the change a line of prose for each one. A
// narrow list costs a cap that still refuses text, which is the fault this whole change is here for.
//
// The three markings are the words the requirement uses: warning, cut for display, and kept because
// something outside demands it.

// theRepositoryRoot is this repository, from this package.
const theRepositoryRoot = "../.."

// enoughCaps is the smallest number of caps this measurement means anything on. The scan found 53 on
// 2 September 2026. A run that finds almost none read the wrong tree, and a test that reads nothing
// passes in silence, which is worse than a test that fails.
const enoughCaps = 15

// theMarkings are the three words a cap may be marked with, and they are the requirement's own words.
var theMarkings = []string{"warning", "cut for display", "kept because"}

// aCapName is a constant that could be a length cap, by its name alone. It is used to read names back
// out of the list, never to find them in the source.
var aCapName = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9]*[Ll]imit\b`)

// lengthCap is one cap the source holds.
type lengthCap struct {
	// Name is the constant, and File is the path of the file that declares it, from the root of the
	// repository.
	Name string
	File string
}

func (c lengthCap) String() string { return c.Name + " in " + c.File }

// TestEveryLengthCapIsNamedInTheList.
//
// The first half of the requirement. One document names every cap the source holds. A cap the list
// leaves out is a cap the operator never reads about.
func TestEveryLengthCapIsNamedInTheList(t *testing.T) {
	caps := theLengthCapsInTheSource(t)
	document, list := theListAnOperatorReads(t, caps)

	missing := make([]string, 0, len(caps))
	for _, one := range caps {
		if len(entriesNamingCap(list, one.Name)) == 0 {
			missing = append(missing, one.String())
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s names %d of the %d length caps in this system, and says nothing about these %d:\n%s",
			document, len(caps)-len(missing), len(caps), len(missing), strings.Join(missing, "\n"))
	}
}

// TestTheListNamesEachCapByItsFile.
//
// The list says where each cap lives. A name on its own sends the operator to grep, and two caps in
// this system are both called Limit, so a name on its own does not even say which one it is.
func TestTheListNamesEachCapByItsFile(t *testing.T) {
	caps := theLengthCapsInTheSource(t)
	document, list := theListAnOperatorReads(t, caps)

	for _, one := range caps {
		named := entriesNamingCap(list, one.Name)
		if len(named) == 0 {
			t.Errorf("%s does not name %s at all", document, one)
			continue
		}
		if entryForCap(named, one) == "" {
			t.Errorf("%s names %s and never says it lives in %s: %q",
				document, one.Name, one.File, named[0])
		}
	}
}

// TestEveryCapInTheListCarriesOneOfTheThreeMarkings.
//
// The second half of the requirement. The list says what each cap became, in one of three words: the
// cap is a warning now, or it is a cut for display, or it is kept because something outside demands
// it. A cap named with no marking tells the operator the cap exists and nothing else.
func TestEveryCapInTheListCarriesOneOfTheThreeMarkings(t *testing.T) {
	caps := theLengthCapsInTheSource(t)
	document, list := theListAnOperatorReads(t, caps)

	for _, one := range caps {
		entry := entryForCap(entriesNamingCap(list, one.Name), one)
		if entry == "" {
			t.Errorf("%s carries no entry for %s, so nothing says what it became", document, one)
			continue
		}
		if markingOfEntry(entry) == "" {
			t.Errorf("the entry for %s says what it is and not what it became: mark it %s. The entry reads %q",
				one, strings.Join(theMarkings, ", or "), entry)
		}
	}
}

// TestACapThatIsKeptSaysWhatDemandsIt.
//
// A cap kept is a cap that still refuses text, so the word costs the most and it needs the most. The
// requirement asks for the thing outside this system that demands the number, such as the ceiling
// another system puts on a label. An entry that says only "kept" is the refusal carried forward with
// nobody accountable for it.
func TestACapThatIsKeptSaysWhatDemandsIt(t *testing.T) {
	caps := theLengthCapsInTheSource(t)
	document, list := theListAnOperatorReads(t, caps)

	kept := 0
	for _, one := range caps {
		entry := entryForCap(entriesNamingCap(list, one.Name), one)
		if entry == "" || markingOfEntry(entry) != "kept because" {
			continue
		}
		kept++
		reason := strings.TrimSpace(afterMarking(entry, "kept because"))
		if len(strings.Fields(reason)) < 3 {
			t.Errorf("%s keeps %s and does not say what demands it: %q", document, one, entry)
		}
	}
	t.Logf("%s keeps %d of %d caps", document, kept, len(caps))
}

// TestTheListNamesNoCapThisSystemNeverHeld.
//
// A list padded with names nobody can find reads as complete and is not. Every cap the list marks is
// a cap the scan found, or a cap the change says it took out. The request is to remove the cap, so an
// entry for a constant that is gone is the list doing its job, and it says the constant is gone.
func TestTheListNamesNoCapThisSystemNeverHeld(t *testing.T) {
	caps := theLengthCapsInTheSource(t)
	document, list := theListAnOperatorReads(t, caps)

	held := map[string]bool{}
	for _, one := range caps {
		held[one.Name] = true
	}
	for _, entry := range list {
		if markingOfEntry(entry) == "" || saysItIsGone(entry) {
			continue
		}
		for _, name := range aCapName.FindAllString(entry, -1) {
			if !held[name] {
				t.Errorf("%s marks %s, and no file in this system declares a length cap of that name: %q",
					document, name, entry)
			}
		}
	}
}

// theListAnOperatorReads finds the document that carries the list, and returns its path and its
// entries. It takes the committed markdown file that names the most caps, because the requirement
// says the list is read and does not say which file holds it.
func theListAnOperatorReads(t *testing.T, caps []lengthCap) (string, []string) {
	t.Helper()

	best, bestNamed := "", 0
	var bestEntries []string
	for _, path := range theProseFiles(t) {
		body, err := os.ReadFile(filepath.Join(theRepositoryRoot, path))
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		found := theEntriesOf(string(body))
		named := 0
		for _, one := range caps {
			if entryForCap(entriesNamingCap(found, one.Name), one) != "" {
				named++
			}
		}
		if named > bestNamed {
			best, bestNamed, bestEntries = path, named, found
		}
	}
	if best == "" {
		t.Fatalf("no committed markdown file names a single one of the %d length caps in this system "+
			"beside the file it lives in. The change carries the list: write it where the pull request "+
			"body carries it, which in this repository is the fragment in changelog.d, and give each cap "+
			"a line that says the constant, its file, and one of %s",
			len(caps), strings.Join(theMarkings, ", or "))
	}
	return best, bestEntries
}

// theLengthCapsInTheSource reads the tree and returns every constant this system compares against the
// length of something.
//
// The definition is mechanical on purpose. A list of caps written by hand is a list that is right on
// the day it is written, and the requirement asks for every cap rather than the ones somebody
// remembered.
func theLengthCapsInTheSource(t *testing.T) []lengthCap {
	t.Helper()

	files := theSourceFiles(t)
	set := token.NewFileSet()
	parsed := map[string]*ast.File{}
	// Where a constant is declared: the directory it is in, then its name, then the file.
	declared := map[string]map[string]string{}
	// What each directory calls itself, so a cap reached as another package's can be found.
	called := map[string]string{}

	for _, path := range files {
		file, err := parser.ParseFile(set, filepath.Join(theRepositoryRoot, path), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		parsed[path] = file
		dir := filepath.Dir(path)
		called[dir] = file.Name.Name
		for name := range numericConstantsIn(file) {
			if declared[dir] == nil {
				declared[dir] = map[string]string{}
			}
			declared[dir][name] = path
		}
	}

	found := map[lengthCap]bool{}
	for path, file := range parsed {
		dir := filepath.Dir(path)
		ast.Inspect(file, func(node ast.Node) bool {
			compared, ok := node.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			switch compared.Op {
			case token.GTR, token.LSS, token.GEQ, token.LEQ:
			default:
				return true
			}
			var against ast.Expr
			switch {
			case measuresALength(compared.X):
				against = compared.Y
			case measuresALength(compared.Y):
				against = compared.X
			default:
				return true
			}
			name, where := resolveCap(against, dir, declared, called)
			if name != "" {
				found[lengthCap{Name: name, File: where}] = true
			}
			return true
		})
	}

	caps := make([]lengthCap, 0, len(found))
	for one := range found {
		caps = append(caps, one)
	}
	sort.Slice(caps, func(i, j int) bool {
		if caps[i].File != caps[j].File {
			return caps[i].File < caps[j].File
		}
		return caps[i].Name < caps[j].Name
	})
	if len(caps) < enoughCaps {
		t.Fatalf("the scan read %d go files and found %d length caps, and this system holds at least %d: the scan is wrong, not the tree",
			len(files), len(caps), enoughCaps)
	}
	names := make([]string, 0, len(caps))
	for _, one := range caps {
		names = append(names, one.String())
	}
	t.Logf("%d length caps:\n%s", len(caps), strings.Join(names, "\n"))
	return caps
}

// measuresALength says whether an expression is the length of something: len of it, or the count of
// its runes.
func measuresALength(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch called := call.Fun.(type) {
	case *ast.Ident:
		return called.Name == "len"
	case *ast.SelectorExpr:
		return called.Sel.Name == "RuneCountInString" || called.Sel.Name == "RuneCount"
	}
	return false
}

// resolveCap turns the side of a comparison into the constant it names and the file that declares it. It
// answers with nothing for a variable, a parameter, a field or a literal, because none of those is a
// cap somebody can write down.
func resolveCap(expr ast.Expr, dir string, declared map[string]map[string]string, called map[string]string) (string, string) {
	switch named := expr.(type) {
	case *ast.Ident:
		if where, ok := declared[dir][named.Name]; ok {
			return named.Name, where
		}
	case *ast.SelectorExpr:
		owner, ok := named.X.(*ast.Ident)
		if !ok {
			return "", ""
		}
		for other, name := range called {
			if name != owner.Name {
				continue
			}
			if where, ok := declared[other][named.Sel.Name]; ok {
				return named.Sel.Name, where
			}
		}
	}
	return "", ""
}

// numericConstantsIn is every constant in a file whose value is a number rather than a word.
func numericConstantsIn(file *ast.File) map[string]bool {
	found := map[string]bool{}
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			valued, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for at, name := range valued.Names {
				if at >= len(valued.Values) {
					continue
				}
				if aNumber(valued.Values[at]) {
					found[name.Name] = true
				}
			}
		}
	}
	return found
}

// aNumber says whether a constant's value is a number. A word, a character and a truth are not caps.
func aNumber(value ast.Expr) bool {
	switch held := value.(type) {
	case *ast.BasicLit:
		return held.Kind == token.INT
	case *ast.BinaryExpr:
		return aNumber(held.X) || aNumber(held.Y)
	case *ast.ParenExpr:
		return aNumber(held.X)
	case *ast.Ident, *ast.SelectorExpr:
		return true
	}
	return false
}

// theSourceFiles is every go file in this repository that a person wrote: no generated code, and no
// tests, because a cap a test declares is not a cap this system holds.
func theSourceFiles(t *testing.T) []string {
	t.Helper()
	return theCommittedFiles(t, func(path string) bool {
		return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") &&
			!strings.HasPrefix(path, "gen/")
	})
}

// theProseFiles is every markdown file committed here.
func theProseFiles(t *testing.T) []string {
	t.Helper()
	return theCommittedFiles(t, func(path string) bool { return strings.HasSuffix(path, ".md") })
}

// theCommittedFiles walks the repository once and keeps what the caller wants.
func theCommittedFiles(t *testing.T, keep func(string) bool) []string {
	t.Helper()

	var found []string
	err := filepath.Walk(theRepositoryRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(theRepositoryRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if info.IsDir() {
			switch relative {
			case ".git", "node_modules", "gen":
				return filepath.SkipDir
			}
			return nil
		}
		if keep(relative) {
			found = append(found, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", theRepositoryRoot, err)
	}
	if len(found) == 0 {
		t.Fatalf("walking %s found no files, so this test read nothing", theRepositoryRoot)
	}
	sort.Strings(found)
	return found
}

// theEntriesOf cuts a markdown document into the entries a reader sees: a bullet and the lines that
// wrap under it, a paragraph, a heading, a row. Prose here wraps at a hundred columns, so a cap and
// its file are often on two lines of one entry.
func theEntriesOf(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var found []string
	var held []string
	flush := func() {
		if len(held) > 0 {
			found = append(found, strings.Join(strings.Fields(strings.Join(held, " ")), " "))
			held = nil
		}
	}
	starts := regexp.MustCompile(`^\s*([-*+]\s|\d+\.\s|#|\|)`)
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if starts.MatchString(line) {
			flush()
		}
		held = append(held, strings.TrimSpace(line))
	}
	flush()
	return found
}

// entriesNamingCap is the entries that name a constant, as a word rather than as part of a longer one.
func entriesNamingCap(entries []string, name string) []string {
	word := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_])`)
	var found []string
	for _, entry := range entries {
		if word.MatchString(entry) {
			found = append(found, entry)
		}
	}
	return found
}

// entryForCap is the entry that names both the cap and the file it lives in, which is the one entry that
// can only be about that cap. Two caps in this system are called Limit.
func entryForCap(entries []string, one lengthCap) string {
	for _, entry := range entries {
		if strings.Contains(entry, one.File) || strings.Contains(entry, filepath.Base(one.File)) {
			return entry
		}
	}
	return ""
}

// markingOfEntry is what an entry says the cap became, or nothing.
func markingOfEntry(entry string) string {
	lower := strings.ToLower(entry)
	for _, marking := range theMarkings {
		if strings.Contains(lower, marking) {
			return marking
		}
	}
	return ""
}

// saysItIsGone is whether an entry says the cap it names was taken out. A cap the change removed is
// not in the source any more, and the list is still right to name it.
func saysItIsGone(entry string) bool {
	lower := strings.ToLower(entry)
	for _, word := range []string{"remove", "gone", "deleted", "no longer", "taken out"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// afterMarking is the text of an entry that follows a marking.
func afterMarking(entry, marking string) string {
	at := strings.Index(strings.ToLower(entry), marking)
	if at < 0 {
		return ""
	}
	return entry[at+len(marking):]
}
