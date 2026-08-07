package manual

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/features"
)

// TestTheManualNamesTheWords. A session told nothing about the crew guesses at the model, and the
// words here are load bearing: a project is not a workspace, and a sandbox is not a session.
func TestTheManualNamesTheWords(t *testing.T) {
	got := Text("quay dispatch <text>")
	for _, word := range []string{"workspace", "project", "thread", "session", "sandbox"} {
		if !strings.Contains(got, word) {
			t.Fatalf("the manual never says %q", word)
		}
	}
	// The distinction the whole model rests on.
	if !strings.Contains(got, "A session runs IN a sandbox") {
		t.Fatalf("the manual does not say how a session and a sandbox relate:\n%s", got)
	}
}

// TestTheManualCarriesTheCommandsItWasGiven, whole and unedited. A second copy of the command list
// would be one more thing to keep in step, and it would be the copy nobody looks at that goes stale.
func TestTheManualCarriesTheCommandsItWasGiven(t *testing.T) {
	commands := "  dispatch [<address>] <text>   start or continue a thread\n  sessions   list sessions"
	got := Text(commands)

	if !strings.Contains(got, strings.TrimSpace(commands)) {
		t.Fatalf("the manual does not carry the commands it was handed:\n%s", got)
	}
}

// TestTheManualIsGeneratedFromTheSpecification, not copied from it. Every feature the binary carries
// has to turn up, or the manual describes a tool that is not the one running.
func TestTheManualIsGeneratedFromTheSpecification(t *testing.T) {
	got := Text("quay")

	all := features.All()
	if len(all) == 0 {
		t.Fatal("the binary carries no specification, so this test proves nothing")
	}
	for _, feature := range all {
		if !strings.Contains(got, feature.Title) {
			t.Fatalf("the manual leaves out %q, so it describes a different tool", feature.Title)
		}
		for _, scenario := range feature.Scenarios {
			if !strings.Contains(got, scenario) {
				t.Fatalf("the manual leaves out the behaviour %q", scenario)
			}
		}
	}
}

// TestTheManualSaysHowToBeTold. The point of it is that a session can be handed something, so it has
// to say how anything gets told anything, and that context is files rather than prompt text.
func TestTheManualSaysHowToBeTold(t *testing.T) {
	got := Text("quay")
	for _, want := range []string{
		"quay context set",
		"/home/agent/workspace",
		"/home/agent/.claude",
		"Context is files",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the manual never says %q, so a session cannot be told anything:\n%s", want, got)
		}
	}
}

// TestTheManualCarriesNoCredential. It is written into a project's context, which is mounted into
// every sandbox in it and readable by everything running there.
func TestTheManualCarriesNoCredential(t *testing.T) {
	got := Text("quay secret set [<workspace>] <key> <value>   set a workspace secret")

	// The command may be named. A value never appears, because this document has never seen one.
	for _, never := range []string{"sk-ant-", "CLAUDE_CODE_OAUTH_TOKEN="} {
		if strings.Contains(got, never) {
			t.Fatalf("the manual carries something that looks like a credential: %q", never)
		}
	}
}
