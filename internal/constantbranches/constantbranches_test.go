package constantbranches_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/constantbranches"
)

// write puts one Go file in a directory of its own and answers with the directory.
func write(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "subject.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write the source: %v", err)
	}
	return dir
}

func scan(t *testing.T, source string) []constantbranches.Finding {
	t.Helper()
	findings, read, err := constantbranches.Scan(write(t, source), constantbranches.Skipped)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if read != 1 {
		t.Fatalf("read %d files, want 1", read)
	}
	return findings
}

func TestItRefusesABranchOnTheLiteralFalse(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f() int {\n\tif false {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")

	if len(findings) != 1 {
		t.Fatalf("found %d branches, want 1: %v", len(findings), findings)
	}
	if findings[0].Literal != "false" {
		t.Errorf("named the literal %q, want false", findings[0].Literal)
	}
	if findings[0].Line != 4 {
		t.Errorf("named line %d, want 4", findings[0].Line)
	}
}

func TestItRefusesABranchOnTheLiteralTrue(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f() int {\n\tif true {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")

	if len(findings) != 1 {
		t.Fatalf("found %d branches, want 1: %v", len(findings), findings)
	}
	if findings[0].Literal != "true" {
		t.Errorf("named the literal %q, want true", findings[0].Literal)
	}
}

func TestItRefusesALiteralBranchInAnElse(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f(n int) int {\n\tif n > 0 {\n\t\treturn 1\n\t} else if false {\n\t\treturn 2\n\t}\n\treturn 0\n}\n")

	if len(findings) != 1 {
		t.Fatalf("found %d branches, want 1: %v", len(findings), findings)
	}
	if findings[0].Line != 6 {
		t.Errorf("named line %d, want 6", findings[0].Line)
	}
}

// The refusal names the file and the line, because a guard that prints an exit code and nothing else
// leaves the next person to find the branch themselves.
func TestTheRefusalNamesTheFileAndTheLine(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f() int {\n\tif false {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")

	printed := findings[0].String()
	if want := "subject.go:4:"; !strings.Contains(printed, want) {
		t.Errorf("the refusal reads %q, and does not carry %q", printed, want)
	}
	if !strings.Contains(printed, "false") {
		t.Errorf("the refusal reads %q, and does not name the literal", printed)
	}
}

func TestItAllowsAnIdentifierThatBeginsWithTheWord(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f(falsePositive, trueValue bool) int {\n\tif falsePositive {\n\t\treturn 1\n\t}\n\tif trueValue {\n\t\treturn 2\n\t}\n\treturn 0\n}\n")

	if len(findings) != 0 {
		t.Errorf("refused a real condition: %v", findings)
	}
}

func TestItAllowsTheWordsInAComment(t *testing.T) {
	findings := scan(t, "package p\n\n// if false { is what this guard refuses.\nfunc f() int {\n\treturn 0\n}\n")

	if len(findings) != 0 {
		t.Errorf("refused a comment: %v", findings)
	}
}

// A test of this guard has to write the forbidden source somewhere. It writes it as a string, and a
// string is data rather than a branch, so the guard reads its own fixtures and correctly says nothing
// about them. That is why no directory of tests is excluded.
func TestItAllowsTheWordsInAString(t *testing.T) {
	findings := scan(t, "package p\n\nconst fixture = \"if false {\\n\\treturn 1\\n}\"\n\nfunc f() string {\n\treturn fixture\n}\n")

	if len(findings) != 0 {
		t.Errorf("refused a string: %v", findings)
	}
}

func TestItAllowsSourceWithNoLiteralBranch(t *testing.T) {
	findings := scan(t, "package p\n\nfunc f(n int) int {\n\tif n > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")

	if len(findings) != 0 {
		t.Errorf("refused clean source: %v", findings)
	}
}

// Reading nothing and reading clean source are the same silence, so the count is what tells them
// apart and the command turns a zero count into a refusal.
func TestItReportsReadingNoGoSource(t *testing.T) {
	_, read, err := constantbranches.Scan(t.TempDir(), constantbranches.Skipped)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if read != 0 {
		t.Fatalf("read %d files, want 0", read)
	}
}

func TestItDoesNotReadTheSkippedDirectories(t *testing.T) {
	root := t.TempDir()
	for _, dir := range constantbranches.Skipped {
		under := filepath.Join(root, dir)
		if err := os.MkdirAll(under, 0o750); err != nil {
			t.Fatalf("make %s: %v", under, err)
		}
		source := "package p\n\nfunc f() int {\n\tif false {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
		if err := os.WriteFile(filepath.Join(under, "skipped.go"), []byte(source), 0o600); err != nil {
			t.Fatalf("write in %s: %v", under, err)
		}
	}

	findings, read, err := constantbranches.Scan(root, constantbranches.Skipped)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("read a skipped directory: %v", findings)
	}
	if read != 0 {
		t.Errorf("read %d files under skipped directories, want 0", read)
	}
}
