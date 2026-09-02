package main

import (
	"fmt"
	"strings"
)

// Building is the environment variable that says this session is building against tests it did not
// write. The system sets it on the task of a worker in the build stage, and on nothing else.
//
// A command line that sets it is refused, whatever else it does, and so is a command line that unsets
// it. A session that could set the variable could put itself outside the boundary, and a boundary a
// session steps out of is advice with extra steps.
//
// The gate is off in every other session on purpose. The stage before this one writes the tests, and a
// gate that refused every session would refuse that worker for doing the thing it was asked to do.
const Building = "KREWE_BUILDING"

// A Refusal is what the session is told instead of what it asked for. Both halves are load bearing: a
// refusal that does not name the way through is a session that tries the next spelling of the same
// thing, and this one has a way through that is not obvious.
type Refusal struct {
	What    string
	Instead string
}

func (r Refusal) String() string { return r.What + "\n\n" + r.Instead }

// theWayThrough is what a worker does with a test it believes is wrong. It is the whole reason this
// gate can refuse without stopping the work: the test may be the thing at fault, and saying so is an
// answer the stage reads, so the session is never stuck between a broken test and a boundary.
const theWayThrough = "You may read this test as much as you need to. You may not change it. If you " +
	"believe the test itself is wrong, say so in your answer, name the file and the assertion, and " +
	"say what you think it should assert. A person decides that. Change the code the test is about " +
	"instead."

// Decide is the whole of the hook. What the session is about to do, and whether it is building.
//
// building is a value rather than a read of the environment, so what the gate refuses is a table
// anybody can read and argue with, rather than behaviour you have to start a container to find out.
//
// holds says whether a path this line takes whole has a test somewhere under it. It is a value for the
// same reason, and the walk that answers it on a real disk has its own cases.
func Decide(tool string, input Input, building bool, holds func(string) bool) (Refusal, bool) {
	// The lift is checked first, and it is checked even where the gate is off. Setting the variable is
	// the one thing that is refused whatever else the line says.
	if refusal, refused := setsTheVariable(input.Command); refused {
		return refusal, true
	}
	if !building {
		return Refusal{}, false
	}
	// A tool that names a file. The field is what is read rather than the name of the tool, because a
	// runtime that adds a write tool this gate has never heard of still sends its path in one of these,
	// and a gate that knew the tools by name would let that one straight through.
	for _, where := range []string{input.FilePath, input.NotebookPath, input.Path} {
		if why, is := APath(where); is {
			return writing(where, why, said(tool)+" writes to it."), true
		}
	}
	if input.Command == "" {
		return Refusal{}, false
	}
	return theCommand(input.Command, holds)
}

// theCommand reads a shell command for the tests it writes to.
func theCommand(command string, holds func(string) bool) (Refusal, bool) {
	written := WrittenBy(command)
	for _, where := range written.Paths {
		if why, is := APath(where); is {
			return writing(where, why, "This command writes to it."), true
		}
	}
	// The words an unknown program was handed, and the words of a whole line where a writer reads its
	// paths from a pipe. They are read as words rather than as paths, so a bare `features` here is a
	// make target and `features/` is the directory of scenarios.
	if len(written.Named) > 0 {
		named := written.Named
		for _, one := range named {
			if one == "" {
				named = append(named, EveryWord(command)...)
				break
			}
		}
		for _, where := range named {
			if why, is := ANamed(where); is {
				return writing(where, why, "This command may write to it."), true
			}
		}
	}
	// A path taken whole. The name of a directory says nothing about what is inside it, so
	// `rm -rf build/` is ordinary work and `rm -rf internal/` takes every test in there with it. What
	// tells those two apart is the disk.
	if holds == nil {
		return Refusal{}, false
	}
	for _, cover := range written.Covers {
		where := cover.Path
		if ATree(where) {
			where = "."
		}
		if !holds(where) {
			continue
		}
		return Refusal{
			What: fmt.Sprintf("%q holds tests, and %q takes it whole. This session is building against "+
				"tests it did not write, so a command that takes a directory of them away, or back to "+
				"another revision, takes the thing holding the requirement.", where, cover.Program),
			Instead: "Name the files you mean, one by one, and leave the tests out. " + theWayThrough,
		}, true
	}
	return Refusal{}, false
}

// said is what to call the tool in a refusal, so a tool this gate has never heard of still reads as
// itself rather than as nothing.
func said(tool string) string {
	if strings.TrimSpace(tool) == "" {
		return "This tool"
	}
	return tool
}

// writing is the refusal a session gets when it is about to change a test.
func writing(where, why, doing string) Refusal {
	return Refusal{
		What: fmt.Sprintf("%s is a test, because %s. %s\n\nThis session is building against tests it "+
			"did not write. A build that changes the test makes the suite agree with the code, and the "+
			"suite is the only thing holding the requirement.", where, why, doing),
		Instead: theWayThrough,
	}
}

// setsTheVariable refuses a session that puts itself inside or outside the boundary.
//
// Every tool rather than the shell alone, because a session that writes the variable into a file the
// next command reads has set it just the same.
func setsTheVariable(command string) (Refusal, bool) {
	if !strings.Contains(command, Building) {
		return Refusal{}, false
	}
	for _, words := range segmentsOf(command) {
		for _, word := range words {
			name, _, assigned := strings.Cut(word, "=")
			if (assigned && name == Building) || word == Building {
				return Refusal{
					What: fmt.Sprintf("This command sets or clears %s, which is the system's to set.",
						Building),
					Instead: "A session that decides its own boundary has no boundary. The system sets " +
						"this on a worker in the build stage. If you believe this session should not be " +
						"under the boundary, say so in your answer.",
				}, true
			}
		}
	}
	return Refusal{}, false
}

// segmentsOf is the words of each command on a line, which is what the check above reads. It is the
// same reading the writes use, with the operators dropped: what it looks for is a word rather than a
// place in a line.
func segmentsOf(line string) [][]string {
	var found [][]string
	var words []string
	for _, one := range tokens(line) {
		if one.operator {
			if len(words) > 0 {
				found = append(found, words)
				words = nil
			}
			continue
		}
		words = append(words, one.text)
	}
	if len(words) > 0 {
		found = append(found, words)
	}
	return found
}
