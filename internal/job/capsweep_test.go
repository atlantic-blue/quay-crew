package job_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every cap the list marks as a warning has stopped refusing text, and the sweep says how many sites
// it found.
//
// The list in changelog.d marks each length cap in this system with what it became: a warning, a cut
// for display, or kept because something outside demands the number. The list is prose, and prose
// costs nothing to write. A cap marked warning in the list and still refusing in the source is worse
// than a cap nobody wrote down, because the operator has read the line that says it was moved.
//
// So the marking is held against the source. A refusal is found rather than listed by hand: a site
// that builds an error naming one of these constants is a site that sends the text back, whatever
// the function is called and whatever file it moves to.
//
// This sweep covers the caps this requirement moves and no others. The caps a person writes to, the
// question and the reading, moved with the verticals beside this one and carry their own tests.

// capThatMoves is one cap, where the source can still refuse at it, and where the list names it.
type capThatMoves struct {
	// What is the kind of text a session or a person writes, in the words the requirement uses.
	What string
	// Names are the constants a refusal measures against. Two packages both call theirs SummaryLimit,
	// so a name is only meaningful beside the directories below.
	Names []string
	// DeclaredIn is the file the list names the cap in, which is how an entry is told from the entry
	// for a cap of the same name in another package.
	DeclaredIn string
	// Sites are the directories a refusal at this cap can live in. It is not always the directory the
	// cap is declared in: a flow measures its sentence against the job package's constant.
	Sites []string
	// Calls are functions whose call is itself the measurement, where the refusal reads as a question
	// rather than as a comparison.
	Calls []string
}

// theCapsThisRequirementMoves is the six kinds of text in the requirement, with the constants that
// still refuse each one.
var theCapsThisRequirementMoves = []capThatMoves{
	{
		What:       "a design reading",
		Names:      []string{"DesignLimit", "DesignLineLimit", "DesignVerticals"},
		DeclaredIn: "internal/job/design.go",
		Sites:      []string{"internal/job"},
	},
	{
		What:       "a handoff",
		Names:      []string{"HandoffLimit"},
		DeclaredIn: "internal/job/ceiling.go",
		Sites:      []string{"internal/job"},
	},
	{
		What:       "a recorded step",
		Names:      []string{"StepLimit", "StepCount"},
		DeclaredIn: "internal/job/resume.go",
		Sites:      []string{"internal/job"},
	},
	{
		What:       "a summary of a hook",
		Names:      []string{"SummaryLimit"},
		DeclaredIn: "internal/hook/hook.go",
		Sites:      []string{"internal/hook"},
	},
	{
		What:       "a summary of a role",
		Names:      []string{"SummaryLimit"},
		DeclaredIn: "internal/role/role.go",
		Sites:      []string{"internal/role"},
	},
	{
		What:       "a summary of a skill",
		Names:      []string{"SummaryLimit"},
		DeclaredIn: "internal/skill/skill.go",
		Sites:      []string{"internal/skill"},
	},
	{
		What:       "a repository address",
		Names:      []string{"Limit", "RepositoryLimit"},
		DeclaredIn: "internal/repository/repository.go",
		Sites:      []string{"internal/repository", "internal/job"},
		Calls:      []string{"TooLong"},
	},
	{
		What:       "a flow graph sentence",
		Names:      []string{"ProductLimit"},
		DeclaredIn: "internal/job/job.go",
		Sites:      []string{"internal/flow"},
	},
}

// aRefusal is one place the source sends text back for its length.
type aRefusal struct {
	Cap      string
	File     string
	Function string
	Line     int
}

func (r aRefusal) String() string {
	return fmt.Sprintf("%s:%d in %s, at %s", r.File, r.Line, r.Function, r.Cap)
}

// The whole of the second half of the requirement: nothing refuses, and the count is reported so the
// operator reading the work knows how wide the sweep was rather than how many lines it changed.
func TestNoSiteStillRefusesTextAtACapTheListMarksAsAWarning(t *testing.T) {
	list := theCapList(t)

	var found []aRefusal
	for _, one := range theCapsThisRequirementMoves {
		if marking := theMarkingOf(list, one); marking != "warning" {
			t.Errorf("the list marks %s in %s as %q, and this requirement moves it to a warning",
				strings.Join(one.Names, " or "), one.DeclaredIn, marking)
		}
		found = append(found, refusalsAt(t, one)...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		return found[i].Line < found[j].Line
	})

	t.Logf("the sweep read %d caps across %d kinds of text and found %d sites",
		theCapsNamed(), len(theCapsThisRequirementMoves), len(found))
	if len(found) > 0 {
		said := make([]string, 0, len(found))
		for _, one := range found {
			said = append(said, one.String())
		}
		t.Fatalf("%d sites still send text back for its length, and the list says each of these caps is a "+
			"warning now:\n%s", len(found), strings.Join(said, "\n"))
	}
}

// theCapsNamed is how many constants the sweep read, for the count the work reports.
func theCapsNamed() int {
	named := 0
	for _, one := range theCapsThisRequirementMoves {
		named += len(one.Names)
	}
	return named
}

// refusalsAt is every place in this cap's directories that builds an error naming it, one per
// function, because a function that refuses twice is one refusal to move.
func refusalsAt(t *testing.T, one capThatMoves) []aRefusal {
	t.Helper()
	var found []aRefusal
	for _, site := range one.Sites {
		for _, path := range theSourceUnder(t, filepath.Join(theRepositoryRoot, site)) {
			found = append(found, refusalsIn(t, one, site, path)...)
		}
	}
	return found
}

// refusalsIn reads one file and returns the functions in it that refuse at this cap.
func refusalsIn(t *testing.T, one capThatMoves, site, path string) []aRefusal {
	t.Helper()
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	named, calls := asSet(one.Names), asSet(one.Calls)
	var found []aRefusal
	for _, declared := range parsed.Decls {
		function, isFunction := declared.(*ast.FuncDecl)
		if !isFunction || function.Body == nil {
			continue
		}
		at := token.NoPos
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall || at != token.NoPos {
				return true
			}
			if buildsAnError(call) && namesOneOf(call.Args, named) {
				at = call.Pos()
			}
			if calls[nameOf(call.Fun)] {
				at = call.Pos()
			}
			return true
		})
		if at != token.NoPos {
			found = append(found, aRefusal{
				Cap:      one.What,
				File:     filepath.Join(site, filepath.Base(path)),
				Function: function.Name.Name,
				Line:     set.Position(at).Line,
			})
		}
	}
	return found
}

// buildsAnError says whether a call makes an error out of what it is given. A warning is written
// with the same words and is not an error, which is the whole difference this requirement turns on.
func buildsAnError(call *ast.CallExpr) bool {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	from, isPackage := selector.X.(*ast.Ident)
	if !isPackage {
		return false
	}
	return (from.Name == "fmt" && selector.Sel.Name == "Errorf") ||
		(from.Name == "errors" && selector.Sel.Name == "New")
}

// namesOneOf says whether any argument of a call names one of these constants.
func namesOneOf(args []ast.Expr, named map[string]bool) bool {
	carries := false
	for _, arg := range args {
		ast.Inspect(arg, func(node ast.Node) bool {
			switch found := node.(type) {
			case *ast.SelectorExpr:
				if named[found.Sel.Name] {
					carries = true
				}
				return false
			case *ast.Ident:
				if named[found.Name] {
					carries = true
				}
			}
			return true
		})
	}
	return carries
}

// nameOf is what a call is called, with the package taken off, and empty for anything that is not a
// plain call by name.
func nameOf(fun ast.Expr) string {
	switch found := fun.(type) {
	case *ast.Ident:
		return found.Name
	case *ast.SelectorExpr:
		return found.Sel.Name
	}
	return ""
}

func asSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, one := range names {
		set[one] = true
	}
	return set
}

// theSourceUnder is every Go file a person wrote under this directory, tests left out: a test that
// asserts the old refusal is a test the work retires, and it is not a site.
func theSourceUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if err != nil {
		t.Fatalf("read the source under %s: %v", root, err)
	}
	if len(found) == 0 {
		t.Fatalf("no Go file under %s, so this sweep read nothing", root)
	}
	return found
}

// theCapList is every line of every committed markdown file under changelog.d, which is where a
// change in this repository writes what it did.
func theCapList(t *testing.T) []string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(theRepositoryRoot, "changelog.d", "*.md"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("read changelog.d: %v", err)
	}
	var lines []string
	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines = append(lines, strings.Split(string(body), "\n")...)
	}
	return lines
}

// theMarkingOf is the word the list marks a cap with, and "nothing" where no line names it beside
// the file it lives in.
func theMarkingOf(list []string, one capThatMoves) string {
	for _, line := range list {
		if !strings.Contains(line, one.DeclaredIn) {
			continue
		}
		if !namesTheCap(line, one.Names) {
			continue
		}
		for _, marking := range []string{"warning", "cut for display", "kept because"} {
			if strings.Contains(line, marking) {
				return marking
			}
		}
		return "nothing"
	}
	return "nothing"
}

// namesTheCap says whether a line of the list is about one of these constants, matched with the
// backticks the list writes them in so RepositoryLimit is never read as Limit.
func namesTheCap(line string, names []string) bool {
	for _, name := range names {
		if strings.Contains(line, "`"+name+"`") {
			return true
		}
	}
	return false
}
