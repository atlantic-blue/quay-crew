// Command changelog assembles this repository's changelog fragments into one dated section.
//
// It prints, and writes nothing. Whoever cuts the release pastes the section under the heading in
// CHANGELOG.md and deletes the fragments in the same commit, so the release is one reviewable change
// by a person rather than a file a command rewrote.
//
// It is a repository tool rather than a crew capability, which is why it is not a quay subcommand: an
// operator's crew has no changelog.d, and a command that cannot work anywhere but here does not
// belong in the binary they install.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/changelog"
)

func main() {
	dir := flag.String("dir", changelog.Dir, "the directory the fragments are in")
	// The date the release lands, so a run can be repeated and read the same. Empty means today.
	date := flag.String("date", "", "the date to head the section with, defaulting to today")
	flag.Parse()

	when := *date
	if when == "" {
		when = time.Now().Format("2 January 2006")
	}

	section, err := changelog.Assemble(*dir, when)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(section)
}
