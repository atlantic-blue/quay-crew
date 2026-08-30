// merge-gate refuses the command that merges.
//
// Every role brief in this system ends a slice the same way: commit, push the branch, open a pull
// request, and never merge. A push applies nothing. A merge runs the pipeline, and the pipeline is
// what spends money and changes infrastructure, so the merge is the operator's gate.
//
// Until this hook, that gate was a sentence in a brief. `may` grants the verbs a session calls on
// the system, and merging is not one of them: it is a github action a session takes with a credential
// a skill gave it. So the one boundary the whole shape rests on was the one thing nothing checked,
// while smaller boundaries were held by a credential.
//
// This is the check. It reads the command the session is about to run, and refuses it if it merges.
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
	os.Exit(Run(os.Stdin, os.Stderr))
}

// Run reads what the runtime sends and answers it. Everything that is not a Bash command about to
// run is allowed, including a payload this hook cannot read: a gate that refuses what it does not
// understand refuses the work, and a broken hook must not be able to stop a system.
func Run(in io.Reader, errs io.Writer) int {
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
	refusal, refused := Decide(event.ToolInput.Command)
	if !refused {
		return 0
	}
	fmt.Fprintln(errs, refusal)
	return Refused
}
