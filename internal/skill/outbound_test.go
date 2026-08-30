package skill

import (
	"strings"
	"testing"
)

// The outbound skill is the one shipped skill that is about a shape rather than a tool. It exists
// because of a deployed page that answered "No video with that id" for a video that was there: the
// code could not read a title out of the watch page, threw the only failure it knew the name of, and
// logged nothing at the boundary, so an hour later what the platform had actually returned was still
// unknown. The operator read a confident false statement and believed it.
//
// Every session holds it, so what its brief says is what a job writing an outbound call is told
// before it writes one.

// What has to be in the log line. The status alone is what was there last time, and it was a guess
// wearing a status code: a consent wall, a refusal and a changed page all arrive as a page with no
// title in it, and only the body tells them apart.
func TestTheShippedOutboundSkillSaysWhatToLogAtTheBoundary(t *testing.T) {
	brief := flowed(shippedSkill(t, "outbound").Brief)

	for _, said := range []string{"status", "size", "body"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says to log the %s, so a failure is read from nothing again", said)
		}
	}
	// A log line is read by whoever holds the logs, which is not always whoever holds the token.
	if !strings.Contains(brief, "never log the credential") {
		t.Error("the brief does not say to keep the credential out of the line, and logging the whole request is the obvious way to log the answer")
	}
}

// The claim the skill exists to stop. A failure the code did not recognise is unknown, and unknown is
// what it is called, rather than the known failure that happens to fit.
func TestTheShippedOutboundSkillRefusesToNameAFailureItDidNotRead(t *testing.T) {
	brief := flowed(shippedSkill(t, "outbound").Brief)

	for _, said := range []string{"unknown", "did not read"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, and naming a failure nobody read is the whole of what this skill is about", said)
		}
	}
	// A distinction that dies inside the code costs nothing and buys nothing. It is worth having only
	// where a person reads it.
	for _, said := range []string{"what the person sees", "wrong confident"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, so the honest outcome can still be rendered as the convenient one", said)
		}
	}
}

// The unknown branch is the one nobody writes a test for, and it is the branch that lied.
func TestTheShippedOutboundSkillAsksForATestOnTheUnknownCase(t *testing.T) {
	brief := flowed(shippedSkill(t, "outbound").Brief)

	if !strings.Contains(brief, "test the unknown case") {
		t.Error("the brief does not ask for a test on the response nobody planned for")
	}
	for _, said := range []string{"empty body", "200"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never names %q as a response to test, and a test tier with no example in it is a sentence", said)
		}
	}
}

// The summary is the whole of what a session pays for on every conversation, and it is what decides
// whether the brief is ever opened. A job about to write an http call has to recognise itself in it.
func TestTheShippedOutboundSkillSummarySaysWhenToReachForIt(t *testing.T) {
	summary := strings.ToLower(shippedSkill(t, "outbound").Summary)

	for _, said := range []string{"outside this process", "log"} {
		if !strings.Contains(summary, said) {
			t.Errorf("the summary does not say %q, so a job writing an outbound call has no reason to open the brief: %q", said, summary)
		}
	}
}

// It must ask for nothing, and that is load bearing rather than tidy. A skill naming a secret the
// workspace has not set is left out of the session entirely, so a skill that named one would be
// missing from exactly the fresh workspace this rule is written for.
func TestTheShippedOutboundSkillAsksForNothing(t *testing.T) {
	outbound := shippedSkill(t, "outbound")

	if len(outbound.Binaries) != 0 {
		t.Errorf("it declares binaries it does not need: %v", outbound.Binaries)
	}
	if len(outbound.Secrets) != 0 {
		t.Errorf("it declares secrets it does not need, and a workspace that has not set one gets no skill: %v", outbound.Secrets)
	}
}
