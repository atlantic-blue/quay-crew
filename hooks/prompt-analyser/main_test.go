package main

import (
	"strings"
	"testing"
)

// The guard is what stops the hook analysing its own model call. Losing it means a hook that calls
// itself, so it is asserted rather than assumed.
func TestTheChildIsGuardedAgainstAnalysingItsOwnPrompt(t *testing.T) {
	child := childEnv([]string{"PATH=/usr/bin", "CLAUDECODE=1", "CLAUDE_CODE_SSE_PORT=1", "HOME=/home/agent"})

	if !has(child, Guard+"=1") {
		t.Errorf("the guard is not set on the child: %v", child)
	}
	if !has(child, "MAX_THINKING_TOKENS=0") {
		t.Errorf("the thinking budget is not zero, which is what makes this fast enough to run: %v", child)
	}
	for _, unwanted := range []string{"CLAUDECODE=1", "CLAUDE_CODE_SSE_PORT=1"} {
		if has(child, unwanted) {
			t.Errorf("the child inherited %q, which the running session set for itself", unwanted)
		}
	}
	if !has(child, "PATH=/usr/bin") || !has(child, "HOME=/home/agent") {
		t.Errorf("the child lost the environment it needs to run at all: %v", child)
	}
}

// In a quay sandbox there is no credentials file: the subscription arrives as this variable, and
// dropping it left the child with nothing to authenticate with. The hook still exited 0 and the only
// sign anywhere was the word "no answer" in a file in /tmp.
func TestTheChildKeepsTheCredentialItNeedsToAuthenticate(t *testing.T) {
	child := childEnv([]string{
		"CLAUDE_CODE_OAUTH_TOKEN=secret",
		"CLAUDE_CONFIG_DIR=/home/agent/.claude",
		"CLAUDE_SOMETHING_ELSE=dropped",
	})

	if !has(child, "CLAUDE_CODE_OAUTH_TOKEN=secret") {
		t.Errorf("the subscription token was dropped, so the child cannot authenticate: %v", child)
	}
	if !has(child, "CLAUDE_CONFIG_DIR=/home/agent/.claude") {
		t.Errorf("the config directory was dropped: %v", child)
	}
	if has(child, "CLAUDE_SOMETHING_ELSE=dropped") {
		t.Errorf("the child inherited a variable the session set for itself: %v", child)
	}
}


// The failure the second name exists for. Claude Code removes CLAUDE_CODE_OAUTH_TOKEN from the
// environment of every process it starts, by that name and no other, so inside a sandbox the hook
// never holds it: keeping it was keeping something that was never there. A real task on 30 August
// 2026 recorded "no answer 770ms" against every message, and the child had exited 1 in 851
// milliseconds with nothing to authenticate with.
func TestTheChildIsGivenTheCredentialUnderTheNameTheCommandLineReads(t *testing.T) {
	child := childEnv([]string{"PATH=/usr/bin", ModelToken + "=from-the-crew", "GH_TOKEN=other"})

	if !has(child, OAuthToken+"=from-the-crew") {
		t.Errorf("the child was never given a credential, so it cannot authenticate: %v", child)
	}
	if count(child, OAuthToken) != 1 {
		t.Errorf("the child holds %s %d times, and which one a process reads is not ours to decide: %v",
			OAuthToken, count(child, OAuthToken), child)
	}
}

// A person on a laptop has the first name and nothing else, and their own value is the one that runs.
func TestTheCredentialAPersonSetsWinsOverTheOneTheCrewCarries(t *testing.T) {
	child := childEnv([]string{OAuthToken + "=mine", ModelToken + "=the-crews"})

	if !has(child, OAuthToken+"=mine") {
		t.Errorf("the credential the person set was replaced: %v", child)
	}
	if count(child, OAuthToken) != 1 {
		t.Errorf("the child holds %s twice: %v", OAuthToken, child)
	}
}

// With no credential anywhere the child gets none, rather than an empty one that reads as configured.
func TestNoCredentialAnywhereHandsTheChildNone(t *testing.T) {
	child := childEnv([]string{"PATH=/usr/bin", "HOME=/home/agent"})

	if count(child, OAuthToken) != 0 {
		t.Errorf("the child was handed a credential nobody set: %v", child)
	}
}

func has(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func count(env []string, name string) int {
	found := 0
	for _, entry := range env {
		if key, _, _ := strings.Cut(entry, "="); key == name {
			found++
		}
	}
	return found
}
