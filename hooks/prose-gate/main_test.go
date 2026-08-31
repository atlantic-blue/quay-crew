package main

import (
	"strings"
	"testing"
)

// The hook as the model runtime runs it: a payload on standard input, an exit code out, and the
// reason on standard error.
//
// Refusing comes first. A gate that always passes satisfies every test about passing.

func TestTheHookRefusesWithTheCodeTheRuntimeReads(t *testing.T) {
	said := &strings.Builder{}
	payload := `{"tool_name":"Write","tool_input":{"file_path":"docs/HOOKS.md","content":"` + too + `"}}`
	if code := Run(strings.NewReader(payload), said); code != Refused {
		t.Fatalf("the hook exited %d, and the runtime reads %d as a refusal", code, Refused)
	}
	for _, needed := range []string{
		"docs/HOOKS.md",
		"Simplified Technical English",
		"words",
		"Split it",
		"https://www.asd-ste100.org/",
	} {
		if !strings.Contains(said.String(), needed) {
			t.Errorf("the refusal does not say %q, so the writer is left guessing:\n%s", needed, said)
		}
	}
}

// The two halves of the standard this gate does not hold, named in the refusal itself. A session
// told only that its prose is wrong rewrites it by guessing, and a session that believes this gate
// is the whole standard believes a passing write is prose in the standard.
func TestTheRefusalSaysWhichHalfOfTheStandardIsNotHeldHere(t *testing.T) {
	said := &strings.Builder{}
	Run(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"a.md","content":"`+too+`"}}`), said)
	for _, needed := range []string{"vocabulary", "idiom"} {
		if !strings.Contains(said.String(), needed) {
			t.Errorf("the refusal does not mention %q, which is a part of the standard a person holds", needed)
		}
	}
}

// A writer handed forty refusals rewrites the whole document by guessing, which is how a session
// burns its budget on prose.
func TestOneFiringReportsAFewRefusalsAndSaysHowManyItKept(t *testing.T) {
	var many strings.Builder
	for at := 0; at < Limit+3; at++ {
		many.WriteString(too + "\n\n")
	}
	said := &strings.Builder{}
	if code := Run(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"a.md","content":`+
		quoteJSON(many.String())+`}}`), said); code != Refused {
		t.Fatalf("the hook exited %d", code)
	}
	if strings.Count(said.String(), "Split it") > Limit {
		t.Errorf("the hook reported more than %d refusals in one firing:\n%s", Limit, said)
	}
	if !strings.Contains(said.String(), "3 more") {
		t.Errorf("the hook did not say how many refusals it kept back:\n%s", said)
	}
}

// It fires on every write and every command a session makes, so anything it cannot read has to go
// through. A gate that refuses what it does not understand refuses the work, and a broken hook must
// not be able to stop a system.
func TestAPayloadTheHookCannotReadLetsTheWriteThrough(t *testing.T) {
	for _, one := range []struct {
		name    string
		payload string
	}{
		{name: "not the payload a runtime sends", payload: "this is not json at all"},
		{name: "nothing at all", payload: ""},
		{name: "a payload naming no tool", payload: `{"tool_input":{"content":"` + too + `"}}`},
		{name: "a tool input of the wrong shape", payload: `{"tool_name":"Write","tool_input":[]}`},
		{
			name:    "prose in a source file",
			payload: `{"tool_name":"Write","tool_input":{"file_path":"a.go","content":"` + too + `"}}`,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			said := &strings.Builder{}
			if code := Run(strings.NewReader(one.payload), said); code != 0 {
				t.Errorf("the hook exited %d and stopped the work: %s", code, said)
			}
		})
	}
}

// quoteJSON is a json string literal, so a test can put a whole document inside a payload.
func quoteJSON(text string) string {
	return `"` + strings.NewReplacer(`"`, `\"`, "\n", `\n`).Replace(text) + `"`
}
