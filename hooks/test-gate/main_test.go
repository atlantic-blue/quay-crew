package main

import (
	"strings"
	"testing"
)

// What the runtime sends, and what it reads back. The exit code is the whole answer: 2 stops the tool
// call, anything else lets it run, so a hook that returns the wrong number on a payload it did not
// expect either stops the session working or stops guarding.
func TestTheHookAnswersWhatTheRuntimeSends(t *testing.T) {
	payloads := []struct {
		name     string
		body     string
		building bool
		want     int
	}{
		{
			name:     "a write to a test is refused",
			body:     `{"tool_name":"Write","tool_input":{"file_path":"internal/job/build_test.go"}}`,
			building: true,
			want:     Refused,
		},
		{
			name:     "a write to the code is not",
			body:     `{"tool_name":"Write","tool_input":{"file_path":"internal/job/build.go"}}`,
			building: true,
			want:     0,
		},
		{
			name:     "reading a test is not",
			body:     `{"tool_name":"Bash","tool_input":{"command":"cat internal/job/build_test.go"}}`,
			building: true,
			want:     0,
		},
		{
			name:     "a shell that writes a test is refused",
			body:     `{"tool_name":"Bash","tool_input":{"command":"echo x > internal/job/build_test.go"}}`,
			building: true,
			want:     Refused,
		},
		{
			name: "a session that is not building is left alone",
			body: `{"tool_name":"Write","tool_input":{"file_path":"internal/job/build_test.go"}}`,
			want: 0,
		},
		// The three below decide whether a broken hook can stop a system. It fires on every write every
		// session makes, so anything it cannot read has to be let through.
		{name: "a payload that is not json", body: "not json at all", building: true, want: 0},
		{name: "a payload with no tool", body: `{"hook_event_name":"PreToolUse"}`, building: true, want: 0},
		{name: "nothing at all", body: "", building: true, want: 0},
	}
	for _, payload := range payloads {
		t.Run(payload.name, func(t *testing.T) {
			said := &strings.Builder{}
			if code := Run(strings.NewReader(payload.body), said, payload.building); code != payload.want {
				t.Fatalf("the hook answered %d, want %d, saying %q", code, payload.want, said)
			}
			// A refusal the session cannot read is a tool call that fails for no stated reason.
			if payload.want == Refused && said.Len() == 0 {
				t.Fatal("the hook refused and told the session nothing")
			}
			if payload.want == 0 && said.Len() != 0 {
				t.Fatalf("the hook allowed the call and said %q", said)
			}
		})
	}
}
