package role

import (
	"path/filepath"
	"strings"
	"testing"
)

// A change tells you what it does, in its commit messages, its description and its comments. Nothing
// in the fifteen roles read that prose against the code it describes, so a run took the author's
// account of the change as a finding about the change.
//
// These are the sentences the claims check is made of. Each one is load bearing. Take the last one
// out and the check returns every claim it read, which is a report nobody opens. They are written
// here rather than derived from the file, because a check that read its expectations out of the
// brief would agree with whatever the brief said.
var claimsCheckMethod = []string{
	// The order. The tracing finishes first, so the claims cannot steer the trace that would have
	// caught them.
	"Read this section last.",

	// The rule the whole check rests on, and the reason a comment is not cover.
	"The narrative is testimony, not evidence.",
	"A claim repeated in a comment is the same",

	// What to pull out of the prose, and what to do with each one.
	"Extract each checkable claim.",
	"Try to falsify each one",

	// This crew's own rule about a rendered sample shown as observed output, which no role stated.
	"A rendered sample shown as observed output is a claim too.",

	// What a falsified claim carries. A claim nobody can locate is a disagreement, not a finding.
	"The file and the line where the code contradicts the claim.",
	"The claim itself, quoted.",
	"What the code does instead.",
	"What goes wrong for a person who believed it.",

	// And the other direction, which is the half a check like this loses first. A check that reports
	// the claims it could not break reports everything, and then it is deleted rather than read.
	"A claim you could not falsify produces nothing.",

	// Where the method came from, and under which licence, in the brief rather than only in the
	// document that chose it.
	"claims-check.md",
	"licensed MIT",
}

// missingClaimsCheck is every sentence of the check a brief does not carry, in the order they are
// declared, so a failure names what to write rather than only that something is absent.
func missingClaimsCheck(brief string) []string {
	var absent []string
	for _, sentence := range claimsCheckMethod {
		if !strings.Contains(brief, sentence) {
			absent = append(absent, sentence)
		}
	}
	return absent
}

// The check that the check works, first, because a guard that cannot fail passes over anything.
//
// A brief with one sentence cut out is what a later edit produces. An empty brief is the other end
// of it: a role whose file failed to load reads as a clean sweep to any check that only asks what is
// absent.
func TestTheClaimsCheckGuardCatchesASentenceTakenOutOfTheBrief(t *testing.T) {
	brief, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}

	for _, sentence := range claimsCheckMethod {
		cut := strings.Replace(brief.Brief, sentence, "", 1)
		if cut == brief.Brief {
			t.Errorf("cutting %q changed nothing, so the guard is looking for a sentence that is not there", sentence)
			continue
		}
		absent := missingClaimsCheck(cut)
		if len(absent) != 1 || absent[0] != sentence {
			t.Errorf("with %q cut, the guard reports %v", sentence, absent)
		}
	}

	if got := len(missingClaimsCheck("")); got != len(claimsCheckMethod) {
		t.Errorf("an empty brief is missing %d of the %d sentences, so a role that failed to load would read as one that carries the check",
			got, len(claimsCheckMethod))
	}
}

// And the file that ships, held to the same guard.
func TestTheVerifierCarriesTheClaimsCheck(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	for _, sentence := range missingClaimsCheck(one.Brief) {
		t.Errorf("the verifier's brief does not say %q, so a session running as it has no claims check", sentence)
	}
	t.Logf("the verifier's brief carries all %d sentences of the claims check", len(claimsCheckMethod))
}

// The claims are read after the tracing, which is an order and not a sentence. Prose cannot enforce
// it, so two things carry it: the instruction to read this section last, and the section itself
// sitting after the tracing method rather than before it. A claims check placed at the top would be
// read first however the sentence above it was worded.
func TestTheClaimsCheckIsReadAfterTheTracing(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	tracing := strings.Index(one.Brief, "<verification_gap>")
	claims := strings.Index(one.Brief, "<claims_check>")
	if tracing < 0 || claims < 0 {
		t.Fatalf("the brief holds the tracing method at %d and the claims check at %d, and needs both", tracing, claims)
	}
	if claims < tracing {
		t.Errorf("the claims check is at %d and the tracing method at %d, so the prose is read the wrong way round",
			claims, tracing)
	}
	if !strings.Contains(one.Brief, "steers the trace") {
		t.Error("the brief orders the two and does not say why, so the next edit moves them back")
	}
}

// The version, because a session is pinned to the version it started with. A brief edited under
// version 2 would change how a session already running as the verifier was told to work, and a
// workspace holding version 2 would go on being given a brief without the claims check, with nothing
// saying so.
//
// It is a floor rather than an equality: the next edit to this brief raises it again.
func TestTheVerifierVersionRoseWithTheClaimsCheck(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	if one.Version < 3 {
		t.Errorf("the verifier ships at version %d, and the brief carrying the claims check is version 3 or above",
			one.Version)
	}
}

// The ceiling. A brief over it is refused at import, so the role would stop shipping at all, and this
// one is now the third closest of the fifteen.
//
// The room left is reported rather than only the size, because the next edit to this file is the one
// that has to know how much there is. The floor is the size of the brief this check was added to, so
// a section deleted rather than edited fails here instead of passing as a smaller file.
func TestTheVerifierBriefWithTheClaimsCheckStaysUnderTheCeiling(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	if len(one.Brief) > BriefLimit {
		t.Errorf("the verifier's brief is %d bytes and the ceiling is %d, so the system refuses it at import",
			len(one.Brief), BriefLimit)
	}
	if len(one.Brief) < 13754 {
		t.Fatalf("the verifier's brief is %d bytes, which is smaller than the one the claims check was added to",
			len(one.Brief))
	}
	t.Logf("the verifier's brief is %d bytes, leaving %d under the ceiling", len(one.Brief), BriefLimit-len(one.Brief))
}
