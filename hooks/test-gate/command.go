package main

import (
	"path"
	"regexp"
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
//
// What decides a line is the program it runs, in three classes. A program that only reads is never
// asked about its paths. A program that writes has every path it was handed read as a path, so a bare
// word is a directory. Anything else is unknown, and only the words that look like paths are read,
// which is what lets `make features` through and stops
// `python3 -c "open('a_test.go','w')"`.

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

// writes are the programs whose whole job is to put something in a file, or to take one away. Every
// path they are handed is a file they may write, and a bare word among them is a path.
var writes = map[string]bool{
	"tee": true, "cp": true, "mv": true, "rm": true, "truncate": true, "dd": true,
	"patch": true, "install": true, "touch": true, "ed": true, "shred": true, "unlink": true,
	"ln": true, "rsync": true, "sponge": true, "rmdir": true, "chmod": true, "chown": true,
}

// inPlace are the programs that read a file and write it back, but only when told to. `sed -n '1,40p'
// a_test.go` is how a session reads a test, which this stage allows on purpose, and `sed -i` on the
// same file is how it rewrites one.
var inPlace = map[string]string{
	"sed": "i", "perl": "i", "ruby": "i", "awk": "i", "gawk": "i",
	"gofmt": "w", "goimports": "w", "prettier": "w", "black": "i", "rustfmt": "i",
}

// interprets are the programs that run code they were handed, so what they write is inside an
// argument rather than beside one. Only the words that look like paths are read out of that code.
var interprets = map[string]bool{
	"python": true, "python3": true, "node": true, "deno": true, "bun": true,
	"php": true, "irb": true, "tclsh": true, "osascript": true,
}

// reads only read. They are never asked about their paths, which is what keeps `go test ./features/`
// and `make features` working while the boundary is on.
var reads = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true, "bat": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "ack": true,
	"wc": true, "diff": true, "cmp": true, "ls": true, "stat": true, "file": true, "du": true,
	"echo": true, "printf": true, "pwd": true, "true": true, "false": true, "test": true,
	"basename": true, "dirname": true, "realpath": true, "which": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "column": true, "date": true, "sleep": true,
	"go": true, "make": true, "cargo": true, "npm": true, "npx": true, "pytest": true,
	"mkdir": true, "cd": true, "export": true, "gh": true, "curl": true, "jq": true,
}

// restores are the version control verbs that put a different copy of a file in the working tree, or
// take an untracked one away. A test changed back to another revision is a test changed.
var restores = map[string]bool{
	"checkout": true, "restore": true, "stash": true, "clean": true, "rm": true, "mv": true,
}

// applies are the programs that bring content from somewhere the command line does not show: an
// archive, a patch, another commit. What they write cannot be read off the line at all, so what they
// are pointed at is read as a directory taken whole.
var applies = map[string]bool{"tar": true, "unzip": true, "patch": true}

// gitApplies are the git verbs that write the working tree out of another commit or a patch. The same
// reading: the files they touch are not on the line.
var gitApplies = map[string]bool{
	"apply": true, "am": true, "cherry-pick": true, "revert": true,
	"merge": true, "pull": true, "rebase": true,
}

// gitReads are the git verbs that write nothing in the working tree.
var gitReads = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true, "grep": true, "ls-files": true,
	"add": true, "commit": true, "push": true, "fetch": true, "remote": true, "branch": true,
	"config": true, "rev-parse": true, "worktree": true, "blame": true, "switch": true,
}

// mutating are the find actions that change something. Without one of these, find only lists.
var mutating = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
}

// pathish finds the words inside an argument that could be a path, so code handed to an interpreter
// is read for the files it names.
var pathish = regexp.MustCompile(`[\w./@+-]*[\w]`)

// Written is every path this command line may write to, and whether the line covers a whole tree.
//
// It answers with paths rather than with a verdict, so what the line does is decided in one place and
// what the gate refuses is decided in another. The caller then asks of each path the one question this
// hook is about, which is whether it is a test.
type Written struct {
	// Paths are the files the line was handed, each read as a path.
	Paths []string
	// Named are the words on the line that look like paths, which is what an unknown program and a
	// writer reading from a pipe are read by.
	Named []string
	// Covers are the paths the line takes whole, which is every path a writer was handed and the
	// working directory where it was handed none. A name says nothing about what is inside a directory,
	// so what is inside is read off the disk.
	Covers []Cover
}

// A Cover is a path a command takes whole, and the command that takes it.
type Cover struct {
	Program string
	Path    string
}

// WrittenBy reads one command line.
func WrittenBy(line string) Written {
	found := Written{}
	writtenBy(line, depth, &found)
	return found
}

func writtenBy(line string, left int, found *Written) {
	if left <= 0 {
		return
	}
	var segment []token
	finish := func() {
		writtenBySegment(segment, left, found)
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
}

// writtenBySegment reads one command: what it redirects into, and what the program itself writes.
func writtenBySegment(segment []token, left int, found *Written) {
	var words []string
	redirected := map[int]bool{}
	for at, one := range segment {
		if !one.operator {
			if !redirected[at] {
				words = append(words, one.text)
			}
			continue
		}
		// The word after a redirect is the file the output lands in, and it is not an argument of the
		// program.
		if redirect[one.text] && at+1 < len(segment) && !segment[at+1].operator {
			found.Paths = append(found.Paths, segment[at+1].text)
			redirected[at+1] = true
		}
	}
	// The redirect target may have been read as a word before its operator was seen.
	for at, one := range segment {
		if redirected[at] {
			words = without(words, one.text)
		}
	}

	program, argv := Program(words)
	if program == "" {
		return
	}
	if inner, isShell := ShellArgument(program, argv); isShell {
		writtenBy(inner, left-1, found)
		return
	}
	switch {
	case program == "git":
		gitWrites(argv, found)
	case program == "find":
		findWrites(argv, found)
	case applies[program] && extracting(program, argv):
		// What it writes is inside an archive or a patch, so what it covers is where it puts it.
		found.Covers = append(found.Covers, Cover{Program: program, Path: into(argv)})
		takes(argv, found, program)
	case writes[program]:
		takes(argv, found, program)
	case inPlace[program] != "" && told(argv, inPlace[program]):
		takes(argv, found, program)
	case inPlace[program] != "":
		// The same program without its flag writes nothing. `sed -n '1,40p' a_test.go` is how a session
		// reads part of a test, which this stage allows on purpose.
	case interprets[program]:
		// What an interpreter writes is inside the code it was handed, so every word in every argument
		// that looks like a path is read.
		for _, word := range argv {
			found.Named = append(found.Named, pathish.FindAllString(word, -1)...)
		}
	case reads[program]:
	default:
		// A program this gate does not know. Only the words that look like paths are read, because a
		// bare word is as likely to be a subcommand or a target as a file.
		found.Named = append(found.Named, paths(argv)...)
	}
}

// takes records what a writing program was handed: each path by its name, and each path again as one
// the command takes whole, because a name says nothing about what is inside a directory.
func takes(argv []string, found *Written, program string) {
	held := paths(argv)
	for _, one := range held {
		found.Covers = append(found.Covers, Cover{Program: program, Path: one})
		if !ATree(one) {
			found.Paths = append(found.Paths, one)
		}
	}
	// A writer with no path of its own reads them from a pipe, so the whole line is read for a path it
	// might have been handed, which is the shape `echo a_test.go` piped into `xargs rm` takes.
	if len(held) == 0 {
		found.Named = append(found.Named, "")
	}
}

// gitWrites reads a git command: the verbs that change the working tree are writers, and the rest read.
func gitWrites(argv []string, found *Written) {
	verb, rest := "", []string(nil)
	for at, word := range argv {
		if strings.HasPrefix(word, "-") {
			continue
		}
		verb, rest = word, argv[at+1:]
		break
	}
	// A verb that writes the working tree out of another commit or a patch. The files it touches are
	// not on the line at all, so what it covers is the working directory.
	if gitApplies[verb] || (verb == "reset" && told(rest, "hard")) {
		found.Covers = append(found.Covers, Cover{Program: "git " + verb, Path: "."})
		return
	}
	if verb == "" || gitReads[verb] || !restores[verb] {
		return
	}
	held := paths(rest)
	if len(held) == 0 {
		// A restore that names no file restores the working directory, and the tests come back with it.
		found.Covers = append(found.Covers, Cover{Program: "git " + verb, Path: "."})
		return
	}
	for _, one := range held {
		found.Covers = append(found.Covers, Cover{Program: "git " + verb, Path: one})
		if !ATree(one) {
			found.Paths = append(found.Paths, one)
		}
	}
}

// findWrites reads a find command. Without a mutating action it lists and nothing else; with one, the
// roots it walks and the names it matches are what it changes.
func findWrites(argv []string, found *Written) {
	acts := false
	for _, word := range argv {
		if mutating[word] {
			acts = true
		}
	}
	if !acts {
		return
	}
	for at, word := range argv {
		switch {
		case word == "-name" || word == "-iname" || word == "-path" || word == "-ipath":
			if at+1 < len(argv) {
				found.Paths = append(found.Paths, argv[at+1])
			}
		case strings.HasPrefix(word, "-"):
		case at == 0 || !strings.HasPrefix(argv[at-1], "-"):
			found.Covers = append(found.Covers, Cover{Program: "find", Path: word})
			if !ATree(word) {
				found.Paths = append(found.Paths, word)
			}
		}
	}
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

// told says whether a program that can edit in place was told to. The flag is one letter in every one
// of them, on its own, joined to a suffix, joined to other letters, or spelled out in a long form.
func told(argv []string, flag string) bool {
	for _, word := range argv {
		if !strings.HasPrefix(word, "-") {
			if word == "inplace" || strings.HasPrefix(word, "inplace=") {
				return true
			}
			continue
		}
		trimmed := strings.TrimLeft(word, "-")
		if strings.HasPrefix(word, "--") {
			// A long flag: --in-place, --in-place=.bak, --write, --inplace.
			trimmed = strings.ReplaceAll(strings.SplitN(trimmed, "=", 2)[0], "-", "")
			if trimmed == "inplace" || (flag == "w" && trimmed == "write") {
				return true
			}
			continue
		}
		if strings.Contains(trimmed, flag) {
			return true
		}
	}
	return false
}

// paths are the arguments that could be a file: the flags are left out, and so is the `--` marker
// that says the rest are paths.
func paths(argv []string) []string {
	var found []string
	for _, word := range argv {
		if word == "" || word == "--" || strings.HasPrefix(word, "-") {
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

// EveryWord is every word of a line that looks like a path, which is what a writer reading from a
// pipe is read by.
func EveryWord(line string) []string {
	var found []string
	for _, one := range tokens(line) {
		if one.operator {
			continue
		}
		found = append(found, pathish.FindAllString(one.text, -1)...)
	}
	return found
}

// extracting says whether this program is about to write what it was handed rather than read it. An
// archive tool has to be told to extract; a patch and a zip write by default.
func extracting(program string, argv []string) bool {
	if program != "tar" {
		return true
	}
	for _, word := range argv {
		trimmed := strings.TrimPrefix(word, "-")
		if trimmed != word && strings.Contains(trimmed, "x") {
			return true
		}
		// The old form, where the flags carry no dash at all: `tar xzf archive.tgz`.
		if trimmed == word && len(word) < 8 && strings.Contains(word, "x") &&
			!strings.Contains(word, ".") && !strings.Contains(word, "/") {
			return true
		}
	}
	return false
}

// into is where a program that unpacks something puts it, which is the working directory unless it was
// told otherwise.
func into(argv []string) string {
	for at, word := range argv {
		if (word == "-C" || word == "-d" || word == "--directory") && at+1 < len(argv) {
			return argv[at+1]
		}
	}
	return "."
}
