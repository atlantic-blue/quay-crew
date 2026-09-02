// process-gate refuses a command that signals or tears down a running process.
//
// A session runs with a shell, and nothing stopped it ending a process it did not start. The
// control plane, the database, the message broker and the operator's terminal all run on the same
// machine as the sandboxes, and the state a person waits on lives in them.
//
// At 13:41 on 1 September 2026 the operator's terminal multiplexer server restarted. Every console
// pane and every conversation pane closed in the same moment, and the build under one of them came
// back as exit code 137. The containers and the recorded sessions survived, so no work was lost. The
// cause was never traced, and no session was shown to have caused it. This gate stands on the harm
// rather than on the blame: the panes were gone at once, and nothing asked first.
//
// A signal is finished before the command returns. There is no review step, no revert and no partial
// application, and everything under the target dies with it. So this reads the command a session is
// about to run, and refuses it when it ends something.
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
// The runtime also takes a refusal as a document on standard output. This one is an exit code
// because that contract is the older and simpler of the two, and a refusal that a runtime does not
// understand is a gate that quietly opens.
const Refused = 2

func main() {
	os.Exit(Run(os.Stdin, os.Stderr, os.Getenv(Lift) != ""))
}

// Run reads what the runtime sends and answers it. Everything that is not a Bash command about to
// run is allowed, including a payload this hook cannot read: a gate that refuses what it does not
// understand refuses the work, and a broken hook must not be able to stop a system.
func Run(in io.Reader, errs io.Writer, lifted bool) int {
	body, err := io.ReadAll(in)
	if err != nil {
		return 0
	}
	var event struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return 0
	}
	refusal, refused := Decide(event.ToolInput.Command, lifted)
	if !refused {
		return 0
	}
	fmt.Fprintln(errs, refusal)
	return Refused
}
