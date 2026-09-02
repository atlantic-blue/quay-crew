package main

import (
	"path"
	"strings"
)

// This file reads a command line the way a shell would, far enough to know which files the line
// writes to. No further: nothing here expands a variable, resolves a glob or runs anything.
//
// Reading it at all is the point. A gate that matched the text of a line would refuse
// `go test ./internal/job/ -run TestBuild` for holding the word test, and a refusal that is wrong
// costs the session a detour and the operator an interruption. Worse, the build stage exists to let a
// worker read the tests, so a gate that stopped a read would take away the thing the boundary was
// deliberately loosened to allow.

// depth bounds the recursion into a substitution or a shell's own argument, so a line built to nest
// forever cannot hold the session's tool call open until the runtime's timeout.
const depth = 8

// token is one word of a command line, and whether the shell reads it as an operator rather than as a
// word. The operators are kept rather than dropped because a redirect is how most of what this gate
// refuses is written: `echo x > a_test.go` runs a program that writes nothing by itself.
type token struct {
	text     string
	operator bool
}

// separator ends one command and starts another.
var separator = map[string]bool{";": true, "&": true, "&&": true, "|": true, "||": true, "\n": true}

// redirect sends what a command prints into a file, so the word after it is a file the line writes.
var redirect = map[string]bool{">": true, ">>": true, "&>": true, ">|": true}

// tokens reads a line into its words, keeping the operators.
func tokens(line string) []token {
	var found []token
	var word strings.Builder
	held := false
	end := func() {
		if held {
			found = append(found, token{text: word.String()})
			word.Reset()
			held = false
		}
	}
	operator := func(text string) {
		end()
		found = append(found, token{text: text, operator: true})
	}

	runes := []rune(line)
	for at := 0; at < len(runes); at++ {
		switch c := runes[at]; c {
		case '\'':
			// Single quotes are literal, so nothing inside one is an operator and nothing inside one runs.
			held = true
			for at++; at < len(runes) && runes[at] != '\''; at++ {
				word.WriteRune(runes[at])
			}
		case '"':
			// Double quotes hold their words together, and a substitution inside one still runs, so what
			// is inside is kept as one word and read again below.
			held = true
			for at++; at < len(runes) && runes[at] != '"'; at++ {
				if runes[at] == '\\' && at+1 < len(runes) {
					at++
				}
				word.WriteRune(runes[at])
			}
		case '\\':
			if at+1 < len(runes) {
				at++
				word.WriteRune(runes[at])
				held = true
			}
		case ' ', '\t':
			end()
		case ';', '\n', '\r', '(', ')', '`', '{', '}':
			operator(string(c))
		case '&', '|', '>', '<':
			// The two character forms, so `>>` is one operator and `&&` is not a redirect.
			text := string(c)
			if at+1 < len(runes) && (runes[at+1] == c || runes[at+1] == '>' || runes[at+1] == '|') {
				at++
				text += string(runes[at])
			}
			operator(text)
		default:
			word.WriteRune(c)
			held = true
		}
	}
	end()
	return found
}

// wrappers are the programs that run another program, so the command that matters is further along
// the line. Each one is unwrapped rather than trusted: a write under sudo is still a write.
var wrappers = map[string]bool{
	"sudo": true, "env": true, "nohup": true, "time": true, "command": true,
	"xargs": true, "timeout": true, "stdbuf": true, "nice": true, "eval": true,
}

// shells run whatever they are handed as one argument, so the argument is a command line of its own.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// keywords are the shell's own words, which stand in front of a command inside a loop or a
// conditional. `for f in a b; do rm $f; done` runs a command in the middle of it, and a reader that
// stopped at `do` would find no program there at all.
var keywords = map[string]bool{
	"do": true, "then": true, "else": true, "elif": true,
	"while": true, "until": true, "if": true, "!": true, "done": true, "fi": true,
}

// writes are the programs whose whole job is to put something in a file. Every path they are handed
// is a file they may write.
var writes = map[string]bool{
	"tee": true, "cp": true, "mv": true, "rm": true, "truncate": true, "dd": true,
	"patch": true, "install": true, "touch": true, "ed": true, "shred": true, "unlink": true,
}

// inPlace are the programs that read a file and write it back, but only when told to. `sed -n '1,40p'
// a_test.go` is how a session reads a test, which this stage allows on purpose, and `sed -i` on the
// same file is how it rewrites one.
var inPlace = map[string]bool{"sed": true, "perl": true, "ruby": true, "awk": true, "gawk": true}

// restores are the version control verbs that put a different copy of a file in the working tree. A
// test changed back to another revision is a test changed.
var restores = map[string]bool{"checkout": true, "restore": true, "stash": true}

// WrittenBy is every path this command line may write to.
//
// It answers with paths rather than with a verdict, so what the gate refuses is decided in one place
// and what a line does is decided in another. The caller then asks of each path the one question this
// hook is about, which is whether it is a test.
func WrittenBy(line string) []string { return writtenBy(line, depth) }

func writtenBy(line string, left int) []string {
	if left <= 0 {
		return nil
	}
	var written []string
	var segment []token
	finish := func() {
		written = append(written, writtenBySegment(segment, left)...)
		segment = nil
	}
	for _, one := range tokens(line) {
		switch {
		case one.operator && separator[one.text]:
			finish()
		case one.operator && (one.text == "(" || one.text == ")" || one.text == "`" ||
			one.text == "{" || one.text == "}"):
			finish()
		default:
			segment = append(segment, one)
		}
	}
	finish()
	return written
}

// writtenBySegment is every path one command writes to: what it redirects into, and what the program
// itself writes.
func writtenBySegment(segment []token, left int) []string {
	var written []string
	var words []string
	for at, one := range segment {
		if !one.operator {
			words = append(words, one.text)
			continue
		}
		// The word after a redirect is the file the output lands in.
		if redirect[one.text] && at+1 < len(segment) && !segment[at+1].operator {
			written = append(written, segment[at+1].text)
		}
	}
	// A redirect's target is the word after it, so it is not an argument of the program.
	for at, one := range segment {
		if one.operator && redirect[one.text] && at+1 < len(segment) && !segment[at+1].operator {
			words = without(words, segment[at+1].text)
		}
	}

	program, argv := Program(words)
	if program == "" {
		return written
	}
	if inner, isShell := ShellArgument(program, argv); isShell {
		return append(written, writtenBy(inner, left-1)...)
	}
	switch {
	case writes[program]:
		written = append(written, paths(argv)...)
	case inPlace[program] && told(argv):
		written = append(written, paths(argv)...)
	case program == "git" && len(argv) > 0 && restores[argv[0]]:
		written = append(written, paths(argv[1:])...)
	}
	return written
}

// without drops the first copy of one word, which is the redirect target already counted.
func without(words []string, target string) []string {
	for at, word := range words {
		if word == target {
			return append(append([]string{}, words[:at]...), words[at+1:]...)
		}
	}
	return words
}

// told says whether a program that can edit in place was told to. The flag is `-i` in every one of
// them, on its own or joined to a suffix or to other letters, so the shape is what this reads.
func told(argv []string) bool {
	for _, word := range argv {
		if !strings.HasPrefix(word, "-") || strings.HasPrefix(word, "--") {
			if word == "inplace" || strings.HasPrefix(word, "inplace=") {
				return true
			}
			continue
		}
		if strings.Contains(strings.TrimPrefix(word, "-"), "i") {
			return true
		}
	}
	return false
}

// paths are the arguments that could be a file: the flags and anything after a bare `--` marker are
// left out, and so is a script handed to sed, which is not a path even when it holds slashes.
func paths(argv []string) []string {
	var found []string
	for _, word := range argv {
		if word == "" || strings.HasPrefix(word, "-") {
			continue
		}
		found = append(found, word)
	}
	return found
}

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
		if wrappers[name] || keywords[name] {
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
