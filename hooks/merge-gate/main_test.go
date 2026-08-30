package main

import (
	"strings"
	"testing"
)

// What the runtime sends, and what it reads back. The exit code is the whole answer: 2 stops the
// command, anything else lets it run, so a hook that returns the wrong number on a payload it did
// not expect either stops the crew working or stops guarding.
func TestTheHookAnswersWhatTheRuntimeSends(t *testing.T) {
	payloads := []struct {
		name string
		body string
		want int
	}{
		{
			name: "a merge is refused",
			body: `{"tool_name":"Bash","tool_input":{"command":"gh pr merge 12 --squash"}}`,
			want: Refused,
		},
		{
			name: "a push is not",
			body: `{"tool_name":"Bash","tool_input":{"command":"git push -u origin work"}}`,
			want: 0,
		},
		// The three below are the ones that decide whether a broken hook can stop a crew. It fires
		// on every Bash command every session runs, so anything it cannot read has to be let
		// through: a gate that refuses what it does not understand refuses the work.
		{name: "a payload that is not json at all", body: "not json", want: 0},
		{name: "a payload with no command in it", body: `{"tool_name":"Bash","tool_input":{}}`, want: 0},
		{name: "nothing", body: "", want: 0},
	}
	for _, one := range payloads {
		t.Run(one.name, func(t *testing.T) {
			var said strings.Builder
			got := Run(strings.NewReader(one.body), &said)
			if got != one.want {
				t.Fatalf("the hook exited %d and the runtime needed %d, on:\n%s", got, one.want, one.body)
			}
			// The reason goes to standard error, which is what the runtime hands the session. A
			// refusal with nothing said is a session that has no idea what happened.
			if one.want == Refused && said.Len() == 0 {
				t.Error("the command was refused and the session was told nothing")
			}
			if one.want == 0 && said.Len() != 0 {
				t.Errorf("the command was allowed and the session was told %q anyway", said.String())
			}
		})
	}
}
