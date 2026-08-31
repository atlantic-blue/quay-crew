package main

import (
	"bytes"
	"strings"
	"testing"
)

// The contract with the model runtime: a payload on standard input, an exit code back, and the reason
// on standard error where the runtime hands it to the session.
func TestARefusalIsAnExitCodeAndAReason(t *testing.T) {
	dir := repository(t)
	write(t, dir, "infra/main.tf", `resource "aws_s3_bucket" "site" {}`)
	commit(t, dir, "feat: the stack")
	// The change is on the default branch itself, so there is nothing to compare against and the diff
	// reads as empty. The denial is the half that still holds, and it is what this fires on.
	payload := `{"tool_name":"Bash","cwd":"` + dir + `","tool_input":{"command":"gh pr create --title x --body 'iam:PassRole implicitDeny'"}}`

	var said bytes.Buffer
	if code := Run(strings.NewReader(payload), &said, dir); code != Refused {
		t.Fatalf("the gate answered %d, and the pull request reports a denied action", code)
	}
	if !strings.Contains(said.String(), "stops the work being ready") {
		t.Errorf("the session is told nothing it can act on: %s", said.String())
	}
}

// It fires on every command every session runs, so anything it cannot read has to go through. A gate
// that refuses what it does not understand refuses the work, and a broken hook must not be able to
// stop a system.
func TestAPayloadTheGateCannotReadLetsTheCommandRun(t *testing.T) {
	for _, payload := range []string{
		"this is not the payload a runtime sends",
		`{"tool_name":"Bash"}`,
		`{"tool_name":"Bash","tool_input":{"command":"ls -la"}}`,
		`{"tool_name":"Read","tool_input":{"file_path":"infra/main.tf"}}`,
		"",
	} {
		var said bytes.Buffer
		if code := Run(strings.NewReader(payload), &said, ""); code != 0 {
			t.Errorf("the gate answered %d to a payload it should have let through: %s\n%s",
				code, payload, said.String())
		}
	}
}
