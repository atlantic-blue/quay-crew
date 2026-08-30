package main

import (
	"fmt"
	"strings"
)

// Decide is the whole of the hook. A command line in, a refusal or nothing out.
//
// It is a pure function of the command, so what the gate refuses is a table anybody can read and
// argue with, rather than behaviour you have to run a container to find out.
func Decide(command string) (Refusal, bool) {
	return decide(command, depth)
}

func decide(command string, left int) (Refusal, bool) {
	if left <= 0 {
		return Refusal{}, false
	}
	for _, words := range Segments(command) {
		program, argv := Program(words)
		// A shell was handed a command line as one argument, so that argument is the command.
		if inner, isShell := ShellArgument(program, argv); isShell {
			if refusal, refused := decide(inner, left-1); refused {
				return refusal, true
			}
			continue
		}
		var refusal Refusal
		var refused bool
		switch program {
		case "gh":
			refusal, refused = gh(argv)
		case "git":
			refusal, refused = push(argv)
		case "curl", "wget":
			refusal, refused = fetch(argv)
		}
		if refused {
			return refusal, true
		}
	}
	return Refusal{}, false
}

// A Refusal is what the session is told instead of what it asked for. Both halves are load bearing:
// a refusal that does not name the way through is a session that tries the next spelling of the same
// command until its budget runs out.
type Refusal struct {
	// What names the command that was refused, in the session's own words.
	What string
	// Instead is what to do rather than that.
	Instead string
}

func (r Refusal) String() string {
	return r.What + " " + r.Instead
}

// theOperators is the second half of every refusal here. One sentence, one place, because a session
// that reads two different explanations of one rule believes it has found an exception.
const theOperators = "A merge runs the pipeline, and the pipeline is what deploys, so the merge is the operator's. " +
	"Push the branch, open a pull request, say its address in your answer, and ask the operator to merge it."

// gh reads a gh command far enough to know whether it merges.
//
// gh's grammar is `gh [global flags] <command> [subcommand] ...`, and the only global flags that
// take a value are --repo and its short form, so skipping those is enough to find the command.
func gh(argv []string) (Refusal, bool) {
	bare := bareWords(argv, map[string]bool{"--repo": true, "-R": true})
	if len(bare) == 0 {
		return Refusal{}, false
	}
	switch bare[0] {
	case "pr":
		if len(bare) > 1 && bare[1] == "merge" {
			return Refusal{What: "`gh pr merge` merges a pull request.", Instead: theOperators}, true
		}
	case "api":
		return api(argv, bare)
	}
	return Refusal{}, false
}

// api catches the same merge asked for over the interface underneath gh's own commands. A gate that
// only knows one spelling is a gate the next spelling walks through.
func api(argv, bare []string) (Refusal, bool) {
	if len(bare) > 1 && bare[1] == "graphql" && mentions(argv, "mergePullRequest") {
		return Refusal{
			What:    "`gh api graphql` with mergePullRequest merges a pull request.",
			Instead: theOperators,
		}, true
	}
	// A read of the merge endpoint asks whether a pull request is merged, and that is a question
	// rather than a merge. Only a write merges.
	if reads(argv) {
		return Refusal{}, false
	}
	for _, word := range bare[1:] {
		if MergeEndpoint(word) {
			return Refusal{
				What:    fmt.Sprintf("`gh api %s` merges a pull request.", word),
				Instead: theOperators,
			}, true
		}
	}
	return Refusal{}, false
}

// fetch catches the merge endpoint called with something that is not gh at all.
func fetch(argv []string) (Refusal, bool) {
	for _, word := range argv {
		if !MergeEndpoint(word) {
			continue
		}
		return Refusal{
			What:    fmt.Sprintf("%s merges a pull request.", word),
			Instead: theOperators,
		}, true
	}
	return Refusal{}, false
}

// push refuses a git push whose destination is the branch a repository merges into. Landing a commit
// on the default branch is what a merge does, so a gate that lets it through is not a gate: it
// refuses the button and leaves the command that does the same thing.
//
// The default branch is read from the two names that are one or the other in practically every
// repository. A repository whose default branch is called something else is a gap, and it is named
// in this hook's README rather than left for somebody to discover.
func push(argv []string) (Refusal, bool) {
	bare := bareWords(argv, map[string]bool{
		"--repo": true, "-o": true, "--push-option": true, "--receive-pack": true, "--exec": true,
	})
	if len(bare) == 0 || bare[0] != "push" {
		return Refusal{}, false
	}
	for _, word := range bare[1:] {
		branch := Destination(word)
		if branch != "main" && branch != "master" {
			continue
		}
		return Refusal{
			What:    fmt.Sprintf("`git push` to %s puts a commit on the branch a pull request merges into.", branch),
			Instead: theOperators,
		}, true
	}
	return Refusal{}, false
}

// Destination is the branch a push refspec lands on, which is the half after the colon when there is
// one. A refspec with no colon lands on the branch it names.
func Destination(refspec string) string {
	trimmed := strings.TrimPrefix(refspec, "+")
	if _, after, found := strings.Cut(trimmed, ":"); found {
		trimmed = after
	}
	return strings.TrimPrefix(trimmed, "refs/heads/")
}

// MergeEndpoint says whether this word addresses the one endpoint that merges a pull request:
// `repos/<owner>/<repo>/pulls/<number>/merge`, with or without a host in front of it.
//
// The number is checked because the shape is what makes it that endpoint. Matching anything ending
// in `/merge` would refuse a call to some other service that happens to spell a path the same way,
// and a gate that refuses work it was never asked to guard is a gate somebody turns off.
func MergeEndpoint(word string) bool {
	path := strings.Trim(strings.Trim(word, "'\""), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[len(parts)-1] != "merge" {
		return false
	}
	return parts[len(parts)-3] == "pulls" && digits(parts[len(parts)-2])
}

func digits(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// reads says whether this gh api call is a read. gh sends GET unless it is told a method or given a
// field, so anything that says neither cannot be writing.
func reads(argv []string) bool {
	writes := map[string]bool{
		"-f": true, "-F": true, "--field": true, "--raw-field": true, "--input": true,
	}
	for at, word := range argv {
		name, value, joined := strings.Cut(word, "=")
		if name == "-X" || name == "--method" {
			if !joined && at+1 < len(argv) {
				value = argv[at+1]
			}
			return strings.EqualFold(value, "get") || strings.EqualFold(value, "head")
		}
		if writes[name] {
			return false
		}
	}
	return true
}

// mentions says whether any word carries this text. It is for a graphql document, which arrives as
// one argument holding a whole query.
func mentions(argv []string, text string) bool {
	for _, word := range argv {
		if strings.Contains(word, text) {
			return true
		}
	}
	return false
}

// bareWords drops the flags, so what is left is the command, its subcommand and its arguments. A
// flag that takes a separate value takes its value with it, or the value reads as a command.
func bareWords(argv []string, valued map[string]bool) []string {
	bare := make([]string, 0, len(argv))
	for at := 0; at < len(argv); at++ {
		word := argv[at]
		if !strings.HasPrefix(word, "-") || word == "-" {
			bare = append(bare, word)
			continue
		}
		name, _, joined := strings.Cut(word, "=")
		if valued[name] && !joined {
			at++
		}
	}
	return bare
}
