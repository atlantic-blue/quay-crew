// deploy-identity-gate refuses a pull request that creates infrastructure until it says the identity
// that will apply it may create it.
//
// A job wrote Terraform for six resources, opened a pull request, and every check went green in
// eleven seconds. The checks were a format check and a validate, and neither one talks to the cloud
// account. The pull request merged and the deploy died on the first command that did: the identity
// that runs it held read only access, and could not have created any of the six.
//
// The rule against that is a skill, and a skill is a rule a session reads. This is the check. It reads
// the command the session is about to run, and refuses it when it opens a pull request that creates
// infrastructure and says nothing about what the deploy identity may do, or when it opens one whose
// body reports an action that came back denied.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Refused is the exit code the model runtime reads as "do not run this, and tell the session why".
// Anything the hook writes to standard error goes to the session as the reason.
const Refused = 2

func main() {
	// The runtime sends where the session is working, and this is the fallback for one that does not.
	here, err := os.Getwd()
	if err != nil {
		here = ""
	}
	os.Exit(Run(os.Stdin, os.Stderr, here))
}

// Run reads what the runtime sends and answers it. Everything that is not a command about to open a
// pull request is allowed, including a payload this hook cannot read: a gate that refuses what it does
// not understand refuses the work, and a broken hook must not be able to stop a system.
func Run(in io.Reader, errs io.Writer, here string) int {
	body, err := io.ReadAll(in)
	if err != nil {
		return 0
	}
	var event struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
		// Where the session is working, which is the repository the change is in. The runtime sends
		// it, and the process directory is the fallback for a runtime that does not.
		Cwd string `json:"cwd"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return 0
	}
	if !OpensAPullRequest(event.ToolInput.Command) {
		return 0
	}
	dir := event.Cwd
	if dir == "" {
		dir = here
	}
	refusal, refused := Decide(event.ToolInput.Command, Changes(dir))
	if !refused {
		return 0
	}
	fmt.Fprintln(errs, refusal)
	return Refused
}
