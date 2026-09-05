// Package constantbranches reads Go source and finds a branch whose condition is a boolean literal.
//
// No linter available here reports one. staticcheck is already enabled through the standard set and
// passes `if false`, and so does every other linter golangci-lint carries with its optional checks
// turned on, measured at version 2.12.2. So the guard is this package rather than a linter entry.
//
// It parses rather than matches text, because the three things a reader expects it to ignore are
// exactly the three a text match gets wrong: the words in a comment, an identifier that begins with
// the word false, and the words inside a string. A parser answers all three by construction, and a
// test fixture may then hold the forbidden source as an ordinary string.
//
// It reads the literal form only. A condition that is always false through a variable, which is the
// shape the dead role path had, still needs a person to see it.
package constantbranches

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Finding is one branch whose condition is a boolean literal.
type Finding struct {
	File    string
	Line    int
	Literal string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: a branch tests the literal %s, so one side of it never runs", f.File, f.Line, f.Literal)
}

// Advice is what to do about every finding, printed once under them.
const Advice = "Delete the branch and keep the side that runs.\n" +
	"If the condition was meant to be real, write the real condition."

// Skipped are the directories the guard does not read.
//
// gen holds generated code, which is not written by hand and not this module's to fix. The hook
// modules are separate Go modules with their own lint pass, and reading them here would report a
// finding in a module this gate does not gate.
var Skipped = []string{"gen", "hooks"}

// Scan reads every Go file under root except the skipped directories, and returns what it found and
// how many files it read.
//
// The count is returned because zero findings and zero files read are the same answer from a caller's
// side, and only one of them means the source is clean.
func Scan(root string, skipped []string) ([]Finding, int, error) {
	skip := make(map[string]bool, len(skipped))
	for _, dir := range skipped {
		skip[dir] = true
	}

	var findings []Finding
	read := 0
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if skip[name] || (strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		read++

		ast.Inspect(file, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}
			if name, literal := booleanLiteral(branch.Cond); literal {
				findings = append(findings, Finding{
					File:    filepath.ToSlash(path),
					Line:    fset.Position(branch.Cond.Pos()).Line,
					Literal: name,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, read, err
	}

	return findings, read, nil
}

// booleanLiteral says whether a condition is the bare word true or false.
//
// The identifier carries no resolved object when it is the predeclared constant, so a local variable
// named true, which is legal Go and pathological, is left to a reader rather than reported here.
func booleanLiteral(cond ast.Expr) (string, bool) {
	ident, ok := cond.(*ast.Ident)
	if !ok {
		return "", false
	}
	if ident.Obj != nil {
		return "", false
	}
	if ident.Name != "true" && ident.Name != "false" {
		return "", false
	}
	return ident.Name, true
}
