package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A test that runs tmux without saying which server it means reaches the server the operator is
// sitting in, because tmux reads TMUX before it reads TMUX_TMPDIR. A kill there ends every window
// and everything running under them, and the run that did it reports only that it was killed.
//
// Two ways out, and a file needs one of them. Name a socket with -L, which is what the feature
// suites do. Or clear TMUX, which points the whole file at TMUX_TMPDIR instead.
//
// This reads the repository rather than one package, because the rule is about the repository and a
// new file is exactly where it comes back.
func TestEveryTestThatRunsTheMultiplexerSaysWhichServerItMeans(t *testing.T) {
	root := repositoryRoot(t)
	const runsIt = `exec.Command("tmux"`
	namesASocket, clearsTheVariable := `"-L"`, `Setenv("TMUX", "")`

	var offenders []string
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "gen" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// This file names the rule, so it holds every string the rule looks for. Counting itself
		// would mean the empty walk below can never fire.
		if filepath.Base(path) == "tmux_isolation_test.go" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if !strings.Contains(text, runsIt) {
			return nil
		}
		checked++
		if strings.Contains(text, namesASocket) || strings.Contains(text, clearsTheVariable) {
			return nil
		}
		here, _ := filepath.Rel(root, path)
		offenders = append(offenders, here)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the repository: %v", err)
	}
	// A walk that finds nothing to check passes exactly as a walk that checked everything does.
	if checked == 0 {
		t.Fatal("no test file runs the multiplexer, so this proves nothing: the search string is wrong")
	}
	if len(offenders) > 0 {
		t.Errorf("these test files run the multiplexer without saying which server they mean, so they "+
			"reach the one the operator is sitting in: %s\n\nName a socket with -L, or clear TMUX with "+
			"t.Setenv(\"TMUX\", \"\").", strings.Join(offenders, ", "))
	}
}

// repositoryRoot walks up from this package until it finds the module file.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	at, err := os.Getwd()
	if err != nil {
		t.Fatalf("asking where this test runs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(at, "go.mod")); err == nil {
			return at
		}
		up := filepath.Dir(at)
		if up == at {
			t.Fatal("no module file above this package, so the repository root is not findable")
		}
		at = up
	}
}
