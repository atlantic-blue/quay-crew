// Command promises refuses a change that touches behaviour and carries neither a changelog entry nor
// a scenario, unless the pull request body says why it has none.
//
// It reads the diff, which is the thing no other check reads. Continuous integration proves the code
// runs; it does not know this repository promises a reader an entry in CHANGELOG.md and a scenario in
// features/ for every behaviour, so that promise held for exactly as long as whoever opened the pull
// request remembered it.
//
// Run it on a branch with no arguments to ask the question before pushing:
//
//	make promises
//
// It is a repository tool rather than a system capability, for the same reason cmd/changelog is: an
// operator's system has no changelog.d and no features directory, and a command that cannot work
// anywhere but here does not belong in the binary they install.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/atlantic-blue/krewe/internal/promise"
)

func main() {
	repo := flag.String("repo", ".", "the repository to read the change in")
	base := flag.String("base", "origin/main", "the ref the change was cut from")
	head := flag.String("head", "HEAD", "the ref the change is on")
	body := flag.String("body", "", "a file holding the pull request body, or - for standard input")
	flag.Parse()

	if err := run(*repo, *base, *head, *body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(repo, base, head, body string) error {
	text, err := readBody(body)
	if err != nil {
		return err
	}

	files, err := promise.Changed(repo, base, head)
	if err != nil {
		return err
	}
	// An empty diff keeps every promise there is, so a run that read nothing is indistinguishable
	// from a change that carried everything. That is almost always the wrong base ref rather than a
	// pull request with no files in it, and it is exactly how a gate reports success for years.
	if len(files) == 0 {
		return fmt.Errorf("%s...%s holds no files, so this check read nothing and proved nothing", base, head)
	}

	findings := promise.Check(promise.Change{Files: files, Body: text})
	if len(findings) == 0 {
		say(files)
		return nil
	}
	for _, finding := range findings {
		fmt.Fprintln(os.Stderr, finding)
	}
	return fmt.Errorf("this change keeps %d of the %d promises this repository makes a reader",
		2-len(findings), 2)
}

// say is what a run that passed leaves behind: the count it read and the reason it let the change
// through. A check that prints nothing when it agrees looks the same as a check that never ran.
func say(files []promise.File) {
	behaviour := promise.Behaviour(files)
	if len(behaviour) == 0 {
		fmt.Printf("read %d changed files, and none of them is behaviour, so this change is asked for neither a changelog entry nor a scenario\n", len(files))
		return
	}
	fmt.Printf("read %d changed files, %d of them behaviour, and this change carries what it promises\n", len(files), len(behaviour))
}

// readBody reads the pull request body: from a file, from standard input when the path is -, and
// nowhere at all when there is no path, which is what a run outside a pull request has.
func readBody(from string) (string, error) {
	switch from {
	case "":
		return "", nil
	case "-":
		text, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading the pull request body from standard input: %w", err)
		}
		return string(text), nil
	default:
		text, err := os.ReadFile(from)
		if err != nil {
			return "", fmt.Errorf("reading the pull request body: %w", err)
		}
		return string(text), nil
	}
}
