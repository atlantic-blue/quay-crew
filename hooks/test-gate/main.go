// test-gate refuses a session that is building against failing tests when it changes one of them.
//
// The build stage hands a worker a suite that is already red and asks it to make the tests pass. The
// shortest way to a green suite is to change the assertion, and a session that does it is not being
// dishonest: a failing test looks exactly like a wrong test from the inside, and the session has no
// way to tell them apart.
//
// The rule was advice for as long as it existed, and advice is what a model weighs against everything
// else it was told. So the boundary is checked at the moment a session tries, by a process the session
// does not control.
//
// It is deliberately looser than the discipline it comes from, where the implementer never sees the
// test at all. Reading is allowed, and allowed on purpose: a build that cannot read the test cannot
// tell a failing assertion from a broken one, and it guesses instead. So this refuses the write and
// nothing else, and it names the way through, which is to say in the answer that the test is wrong.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Refused is the exit code the model runtime reads as "do not run this, and tell the session why".
// Anything the hook writes to standard error goes to the session as the reason.
//
// The runtime also takes a refusal as a document on standard output. This one is an exit code because
// that contract is the older and simpler of the two, and a refusal that a runtime does not understand
// is a gate that quietly opens.
const Refused = 2

// Input is the part of what the runtime sends that says which file is about to change: the path a
// write or an edit names, the notebook a notebook edit names, the bare path some tools use, and the
// command a shell runs.
//
// Every field a tool could put a path in is read rather than the tool being recognised by name. A
// runtime that adds a write tool this gate has never heard of still sends its path in one of these,
// and a gate that knew the tools by name would let that one through.
type Input struct {
	Command      string `json:"command"`
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Path         string `json:"path"`
}

func main() {
	os.Exit(Run(os.Stdin, os.Stderr, os.Getenv(Building) != ""))
}

// Run reads what the runtime sends and answers it. Everything that is not a write this hook
// understands is allowed, including a payload it cannot read: a gate that refuses what it does not
// understand refuses the work, and a broken hook must not be able to stop a system.
func Run(in io.Reader, errs io.Writer, building bool) int {
	body, err := io.ReadAll(in)
	if err != nil {
		return 0
	}
	var event struct {
		ToolName  string `json:"tool_name"`
		ToolInput Input  `json:"tool_input"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return 0
	}
	refusal, refused := Decide(event.ToolName, event.ToolInput, building, HoldsATest)
	if !refused {
		return 0
	}
	fmt.Fprintln(errs, refusal)
	return Refused
}
