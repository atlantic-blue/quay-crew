package main

import (
	"path"
	"strings"
)

// This file reads a command line the way a shell would, far enough to know which programs it runs
// and with what arguments. No further: nothing here expands a variable, resolves a glob or runs
// anything.
//
// It is the merge gate's reader, carried across rather than shared. A hook is its own module by
// design, so it cannot import another hook, and one copy each is the price of that. The alternative
// is a library both depend on, which makes a hook part of the system again.
//
// Reading it at all is the point. A gate that matched text would refuse a command that merely says
// the words, and a refusal that is wrong costs the operator an interruption, which is worse than no
// gate. Quoting is what tells those apart: the words of a quoted argument are one token and can
// never be a command.

// depth bounds the recursion into a substitution or a shell's own argument, so a line built to nest
// forever cannot hold the session's tool call open until the runtime's timeout.
const depth = 8

// Segments are the commands a line runs, each as its words. Separators between them are gone: what
// comes back is one entry per command, whether the line joined them with a pipe, a semicolon, an
// and, a substitution or a shell of its own.
func Segments(line string) [][]string {
	return segments(line, depth)
}

func segments(line string, left int) [][]string {
	if left <= 0 {
		return nil
	}
	var found [][]string
	var current []string
	var token strings.Builder
	held := false

	end := func() {
		if held {
			current = append(current, token.String())
			token.Reset()
			held = false
		}
	}
	breakHere := func() {
		end()
		if len(current) > 0 {
			found = append(found, current)
			current = nil
		}
	}

	runes := []rune(line)
	for at := 0; at < len(runes); at++ {
		switch c := runes[at]; c {
		case '\'':
			// Single quotes are literal, so nothing inside one is a separator and nothing inside one
			// runs.
			held = true
			for at++; at < len(runes) && runes[at] != '\''; at++ {
				token.WriteRune(runes[at])
			}
		case '"':
			// Double quotes hold their words together, and a substitution inside one still runs, so
			// what is inside is kept as one token and read again below.
			held = true
			for at++; at < len(runes) && runes[at] != '"'; at++ {
				if runes[at] == '\\' && at+1 < len(runes) {
					at++
				}
				token.WriteRune(runes[at])
			}
		case '\\':
			if at+1 < len(runes) {
				at++
				token.WriteRune(runes[at])
				held = true
			}
		case ' ', '\t':
			end()
		case ';', '&', '|', '\n', '\r', '(', ')', '`', '{', '}', '<', '>':
			breakHere()
		default:
			token.WriteRune(c)
			held = true
		}
	}
	breakHere()

	// A substitution inside double quotes is a command that still runs, so it is read again. Only a
	// substitution: a quoted argument that merely holds a semicolon is data, and reading it as a
	// command line is how a gate refuses a commit message.
	var nested [][]string
	for _, words := range found {
		for _, word := range words {
			if !strings.Contains(word, "$(") && !strings.Contains(word, "`") {
				continue
			}
			nested = append(nested, segments(strings.NewReplacer("$(", " ", ")", " ", "`", " ").Replace(word), left-1)...)
		}
	}
	return append(found, nested...)
}

// wrappers are the programs that run another program, so the command that matters is further along
// the line. Each one is unwrapped rather than trusted: `sudo gh pr merge` is a merge.
var wrappers = map[string]bool{
	"sudo": true, "env": true, "nohup": true, "time": true, "command": true,
	"xargs": true, "timeout": true, "stdbuf": true, "nice": true, "eval": true,
}

// shells run whatever they are handed as one argument, so the argument is a command line of its own.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// Program is what a segment runs and what it runs it with, with the wrappers, the flags of those
// wrappers and any leading assignment taken off.
func Program(words []string) (string, []string) {
	for at := 0; at < len(words); at++ {
		word := words[at]
		// FOO=bar in front of a command sets a variable for it and is not the command.
		if before, _, found := strings.Cut(word, "="); found && before != "" && !strings.HasPrefix(word, "-") {
			continue
		}
		name := path.Base(word)
		if wrappers[name] {
			continue
		}
		if strings.HasPrefix(word, "-") {
			continue
		}
		return name, words[at+1:]
	}
	return "", nil
}

// ShellArgument is the command line a shell was handed with -c, if this is a shell being handed one.
func ShellArgument(program string, argv []string) (string, bool) {
	if !shells[program] {
		return "", false
	}
	for at, word := range argv {
		if word == "-c" && at+1 < len(argv) {
			return argv[at+1], true
		}
	}
	return "", false
}
