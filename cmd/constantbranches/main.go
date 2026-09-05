// Command constantbranches refuses a branch whose condition is a boolean literal.
//
// Run it over the module before pushing:
//
//	make constant-branches
//
// It is a repository tool rather than a system capability, for the same reason promises is: an
// operator's installed system has no source tree to read.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/atlantic-blue/quay-krewe/internal/constantbranches"
)

func main() {
	root := flag.String("root", ".", "the directory to read Go source under")
	flag.Parse()

	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root string) error {
	findings, read, err := constantbranches.Scan(root, constantbranches.Skipped)
	if err != nil {
		return err
	}

	// A guard that read nothing reports the same silence as a guard over clean source, so it says so
	// rather than passing. A moved directory or a wrong root would otherwise read as a clean run.
	if read == 0 {
		return fmt.Errorf("read no Go source under %s, so this guard proved nothing", root)
	}

	if len(findings) > 0 {
		for _, finding := range findings {
			fmt.Fprintln(os.Stderr, finding)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, constantbranches.Advice)
		branches := "branches test"
		if len(findings) == 1 {
			branches = "branch tests"
		}
		return fmt.Errorf("%d %s a boolean literal, in %d files read", len(findings), branches, read)
	}

	fmt.Printf("no branch tests a boolean literal, in %d files read\n", read)
	return nil
}
