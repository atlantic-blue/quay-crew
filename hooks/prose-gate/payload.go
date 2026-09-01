package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// This file finds the prose in what the runtime sends, and finds nothing anywhere else.
//
// Two shapes carry prose. A file being written, where the prose is the content and the file name
// says whether it is prose at all. And a command being run, where the prose is an argument: a pull
// request body, an issue body, a commit message, and the same text handed over as a file.
//
// Everything else is left alone, and that is the whole reason this gate can be attached. A Go file
// is not prose, and a gate that measured sentence length in source would refuse every file in the
// repository on its first firing, which is a gate somebody turns off and takes the real rule with.

// A Piece is one run of prose and where it came from, so a refusal says which file or which argument
// it is about.
type Piece struct {
	Where string
	Text  string
}

// proseFiles are the file types this gate reads. Everything else is source, data or configuration.
var proseFiles = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true,
}

// Pieces are the prose in one tool call, or nothing when the call carries none.
//
// read is how a file named on a command line is fetched, which is the operating system in the hook
// and a table in the tests. A file that cannot be read is prose this gate did not see, and it goes
// through: a gate that refuses what it cannot read refuses the work.
func Pieces(tool string, input json.RawMessage, read func(string) ([]byte, error)) []Piece {
	switch tool {
	case "Write":
		var written struct {
			Path    string `json:"file_path"`
			Content string `json:"content"`
		}
		if json.Unmarshal(input, &written) != nil || !IsProse(written.Path) {
			return nil
		}
		return []Piece{{Where: written.Path, Text: written.Content}}
	case "Edit":
		var edited struct {
			Path string `json:"file_path"`
			New  string `json:"new_string"`
		}
		if json.Unmarshal(input, &edited) != nil || !IsProse(edited.Path) {
			return nil
		}
		return []Piece{{Where: edited.Path, Text: edited.New}}
	case "MultiEdit":
		var edited struct {
			Path  string `json:"file_path"`
			Edits []struct {
				New string `json:"new_string"`
			} `json:"edits"`
		}
		if json.Unmarshal(input, &edited) != nil || !IsProse(edited.Path) {
			return nil
		}
		pieces := make([]Piece, 0, len(edited.Edits))
		for _, one := range edited.Edits {
			pieces = append(pieces, Piece{Where: edited.Path, Text: one.New})
		}
		return pieces
	case "Bash":
		var run struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &run) != nil {
			return nil
		}
		return commandPieces(run.Command, read)
	}
	return nil
}

// IsProse says whether a file is prose a person reads. It is the file type and nothing else: reading
// the content to decide would make the gate's answer depend on what is being written, and a writer
// could then get a paragraph through by opening it with a line of source.
func IsProse(path string) bool {
	return proseFiles[strings.ToLower(filepath.Ext(path))]
}

// carriers are the flags that carry prose, against the program they belong to, and whether the value
// is the text or the name of a file holding it.
//
// A flag rather than a position, because that is what makes this narrow. `gh pr create --title` is
// prose too and it is not here: a title is a line, the sentence rules are about paragraphs, and a
// gate that refuses a title has started measuring things nobody asked it to.
var carriers = map[string]bool{
	"--body": false, "-b": false, "--message": false, "-m": false, "--notes": false,
	"--body-file": true, "-F": true, "--file": true, "--notes-file": true,
}

// commandPieces are the prose a command line carries as an argument.
func commandPieces(command string, read func(string) ([]byte, error)) []Piece {
	return commandPiecesTo(command, read, depth)
}

func commandPiecesTo(command string, read func(string) ([]byte, error), left int) []Piece {
	if left <= 0 {
		return nil
	}
	var pieces []Piece
	for _, words := range Segments(command) {
		program, argv := Program(words)
		if inner, isShell := ShellArgument(program, argv); isShell {
			pieces = append(pieces, commandPiecesTo(inner, read, left-1)...)
			continue
		}
		if !writesProse(program, argv) {
			continue
		}
		pieces = append(pieces, flagPieces(program, argv, read)...)
	}
	return pieces
}

// writesProse says whether this command is one that carries prose for a person to read.
//
// `gh api` is deliberately not here. Its -F means a field rather than a file, so reading it as one
// would send this gate looking for a file that is a key and a value, and a body posted through the
// interface underneath gh is a shape this gate does not claim to hold. That gap is in its README.
func writesProse(program string, argv []string) bool {
	switch program {
	case "git":
		return len(argv) > 0 && (first(argv) == "commit" || first(argv) == "tag")
	case "gh":
		switch first(argv) {
		case "pr", "issue", "release":
			return true
		}
	}
	return false
}

// first is the first word of a command that is not a flag, which is its subcommand.
func first(argv []string) string {
	for _, word := range argv {
		if !strings.HasPrefix(word, "-") {
			return word
		}
	}
	return ""
}

// flagPieces are the values of the flags that carry prose, as prose.
func flagPieces(program string, argv []string, read func(string) ([]byte, error)) []Piece {
	var pieces []Piece
	for at := 0; at < len(argv); at++ {
		name, value, joined := strings.Cut(argv[at], "=")
		isFile, carries := carriers[name]
		if !carries {
			continue
		}
		if !joined {
			if at+1 >= len(argv) {
				continue
			}
			at++
			value = argv[at]
		}
		where := program + " " + name
		if !isFile {
			pieces = append(pieces, Piece{Where: where, Text: value})
			continue
		}
		// A body handed over on standard input is written "-", and there is no file to read.
		if value == "-" || value == "" {
			continue
		}
		body, err := read(value)
		if err != nil {
			continue
		}
		pieces = append(pieces, Piece{Where: value, Text: string(body)})
	}
	return pieces
}
