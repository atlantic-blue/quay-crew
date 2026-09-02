package main

import (
	"strings"
	"testing"
)

// What the runtime sends, and what it reads back. The exit code is the whole answer: 2 stops the
// command, anything else lets it run, so a hook that returns the wrong number on a payload it did
// not expect either stops the system working or stops guarding.
func TestTheHookAnswersWhatTheRuntimeSends(t *testing.T) {
	payloads := []struct {
		name   string
		body   string
		lifted bool
		want   int
	}{
		{
			name: "a signal is refused",
			body: `{"tool_name":"Bash","tool_input":{"command":"kill -9 4213"}}`,
			want: Refused,
		},
		{
			name: "the terminal is refused",
			body: `{"tool_name":"Bash","tool_input":{"command":"tmux kill-server"}}`,
			want: Refused,
		},
		{
			name: "ending a job is not",
			body: `{"tool_name":"Bash","tool_input":{"command":"krewe job stop 31a6d96d"}}`,
			want: 0,
		},
		{
			name:   "the operator lifted the gate for this session",
			body:   `{"tool_name":"Bash","tool_input":{"command":"kill -9 4213"}}`,
			lifted: true,
			want:   0,
		},
		// The three below are the ones that decide whether a broken hook can stop a system. It fires
		// on every Bash command every session runs, so anything it cannot read has to be let
		// through: a gate that refuses what it does not understand refuses the work.
		{name: "a payload that is not json at all", body: "not json", want: 0},
		{name: "a payload with no command in it", body: `{"tool_name":"Bash","tool_input":{}}`, want: 0},
		{name: "nothing", body: "", want: 0},
	}
	for _, one := range payloads {
		t.Run(one.name, func(t *testing.T) {
			var said strings.Builder
			got := Run(strings.NewReader(one.body), &said, one.lifted)
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
