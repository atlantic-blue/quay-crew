// Package repository holds what the system knows about a repository address, and what kind of
// repository it is.
//
// Two things name a repository now: a job says where its work goes, and a project says where its
// work lands. The address rules are one rule, so they live here and both callers hold to them. A
// second spelling of "this is an owner and a name" is a second answer to the same question, and the
// two drift on the day somebody fixes one of them.
package repository

import (
	"fmt"
	"regexp"
	"strings"
)

// Limit is how long a repository address may be. It is two path segments and a host, so it is
// nowhere near a title, and a value longer than this is a paste of something else.
const Limit = 200

// shape is what a repository address has to look like: two segments, owner and name.
//
// The characters are the ones a forge allows in either segment. A segment that starts or ends with a
// separator is refused, which is what keeps a stray slash or a trailing dot out of an address the
// system will later go looking for in an answer.
var shape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Tidy is the address as it is stored: owner/name, with the spellings that arrive from a browser's
// address bar taken back down to it.
//
// Both spellings are accepted because both are what somebody has in front of them. A person copying
// from a browser has the whole address, a person typing from memory has owner/name, and refusing
// either would be a refusal whose fix is obvious and therefore a refusal not worth having.
func Tidy(address string) string {
	tidy := strings.TrimSpace(address)
	tidy = strings.TrimSuffix(tidy, "/")
	if scheme := strings.Index(tidy, "://"); scheme >= 0 {
		tidy = tidy[scheme+len("://"):]
		if host := strings.Index(tidy, "/"); host >= 0 {
			tidy = tidy[host+1:]
		}
	}
	return strings.TrimSuffix(tidy, ".git")
}

// Shaped says whether an address is an owner and a name and nothing else.
func Shaped(address string) bool { return shape.MatchString(address) }

// TooLong says whether an address is longer than anything a repository address is.
func TooLong(address string) bool { return len(address) > Limit }

// The two kinds of repository the system knows, and the words the operator types.
//
// Public is the default everywhere one is chosen. A pipeline's minutes are free on a public
// repository and metered on a private one, which is the cost rule that was living in a person's head
// and being said out loud once per project.
const (
	Public  = "public"
	Private = "private"
)

// Kind is the visibility a word names, and public where nothing was said.
//
// A word that is neither is refused rather than taken for the default: "internal" and "unlisted" are
// both things a forge has, and quietly recording either as public would be the system writing down a
// cost fact nobody told it.
func Kind(word string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "":
		return Public, nil
	case Public:
		return Public, nil
	case Private:
		return Private, nil
	default:
		return "", fmt.Errorf("a repository is %s or %s, and %q is neither", Public, Private, word)
	}
}

// Usable refuses an address the system could not act on.
//
// Held to the shape at the write, while the person who typed it is looking, because the alternative
// is work that runs for an hour and then stops on an address that was never going to match anything.
func Usable(address string) error {
	if TooLong(address) {
		return fmt.Errorf("the repository is %d bytes and the ceiling is %d: a repository is an owner and a "+
			"name, so write it as atlantic-blue/quay-crew", len(address), Limit)
	}
	if !Shaped(address) {
		return fmt.Errorf("%q is not an owner and a name: write it as atlantic-blue/quay-crew, or paste "+
			"the address of the repository", address)
	}
	return nil
}

// Costs is what this kind of repository does to the bill, said in the same breath as the address so
// the choice is read rather than remembered.
//
// It is the whole reason the kind is recorded. The acceptance run had the operator say "it should be
// a public repository so we can use the CI", which is a cost rule that holds for every project this
// system will ever run and was written down nowhere.
func Costs(visibility string) string {
	if visibility == Private {
		return "a private repository, so its pipeline minutes are metered"
	}
	return "a public repository, so its pipeline minutes are free"
}
