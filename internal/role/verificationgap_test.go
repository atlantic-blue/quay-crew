package role

import (
	"path/filepath"
	"strings"
	"testing"
)

// The verifier asks whether a slice works and not only whether its suite is green. Until now the
// brief said that and gave no method for answering it, so the answer came out of whatever the model
// already believed about tests.
//
// These are the sentences the method is made of. Each one is load bearing: take any of them out and
// a session reads the role as an instruction to count tests. They are written here rather than
// derived from the file, because a check that read its expectations out of the brief would agree
// with whatever the brief said.
var verificationGapMethod = []string{
	// The one question. Everything under it is a way of answering it.
	"If the behaviour this change produces broke where it is used, would verification fail?",

	// The three shapes a gap takes.
	"A regression gap.",
	"A missing adoption gap.",
	"A broken verification gap.",

	// What does not count as a test. These four are the shapes that ship wrong: each one passes on
	// the day it is written and stays green after the behaviour breaks.
	"A test that runs the changed code and never checks the changed result.",
	"A test that mocks away the integration the change is about.",
	"A check that only asserts that no error was thrown.",
	"An assertion against source text rather than against a run.",

	// The evidence rules, which are this crew's rule about proving a check ran, written as
	// instructions a session follows.
	"Read a test before you say what it covers.",
	"Search the whole repository before you say no test exists.",
	"Say how far you looked, inside the finding.",
	"Never assert what you did not verify.",

	// What a finding carries. A gap nobody can locate and nobody can repeat is a sentence, not a
	// finding.
	"The file and the line of the behaviour that nothing protects.",
	"The search that grounds it",

	// Where the method came from, and under what licence. It is in the brief rather than only in
	// docs/ROLE-IMPORTS.md so that a reader of the role reads it.
	"bmad-code-org/BMAD-METHOD",
	"licensed MIT",
	"BMad Code, LLC",

	// The role still changes no file. The method adds reading to do, and a verifier that started
	// fixing what it found would be the author of the code it then judges.
	"You never modify code, tests, or any files.",
}

// missingMethod is every sentence of the method a brief does not carry, in the order they are
// declared, so a failure names what to write rather than only that something is absent.
func missingMethod(brief string) []string {
	var absent []string
	for _, sentence := range verificationGapMethod {
		if !strings.Contains(brief, sentence) {
			absent = append(absent, sentence)
		}
	}
	return absent
}

// The check that the check works, first, because a guard that cannot fail passes over anything.
//
// A brief with one sentence cut out is exactly what a later edit produces, so the guard is watched
// catching that before it is trusted about the file that ships. An empty brief is the other end of
// it: a role whose file failed to load reads as a clean sweep to any check that only asks what is
// absent.
func TestTheMethodCheckCatchesASentenceTakenOutOfTheBrief(t *testing.T) {
	brief, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}

	for _, sentence := range verificationGapMethod {
		cut := strings.Replace(brief.Brief, sentence, "", 1)
		if cut == brief.Brief {
			t.Errorf("cutting %q changed nothing, so the guard is looking for a sentence that is not there", sentence)
			continue
		}
		absent := missingMethod(cut)
		if len(absent) != 1 || absent[0] != sentence {
			t.Errorf("with %q cut, the guard reports %v", sentence, absent)
		}
	}

	if got := len(missingMethod("")); got != len(verificationGapMethod) {
		t.Errorf("an empty brief is missing %d of the %d sentences, so a role that failed to load would read as one that carries the method",
			got, len(verificationGapMethod))
	}
}

// And the file that ships, held to the same guard.
func TestTheVerifierCarriesTheVerificationGapMethod(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	for _, sentence := range missingMethod(one.Brief) {
		t.Errorf("the verifier's brief does not say %q, so a session running as it has no method for that", sentence)
	}
	t.Logf("the verifier's brief carries all %d sentences of the method", len(verificationGapMethod))
}

// The version, because a session is pinned to the version it started with. A brief edited under
// version 1 would change how a session already running as the verifier was told to work, and a
// workspace holding version 1 would go on being given the old method with nothing saying so.
//
// It is a floor rather than an equality: the next edit to this brief raises it again, and a test that
// demanded 2 forever would be a test somebody deletes rather than reads.
func TestTheVerifierVersionRoseWithItsBrief(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	if one.Version < 2 {
		t.Errorf("the verifier ships at version %d, and the brief carrying the verification gap method is version 2 or above",
			one.Version)
	}
}

// The ceiling. The method is about three and a half thousand bytes of new instruction, and a brief
// over the ceiling is refused at import, so the role would stop shipping at all.
//
// The room left is reported rather than only the size, because the next edit to this file is the one
// that has to know how much there is. A floor is asserted too: a brief that failed to load is nought
// bytes, which is under any ceiling.
func TestTheVerifierBriefStaysUnderTheCeiling(t *testing.T) {
	one, err := One(filepath.Join(shipped, "verifier"))
	if err != nil {
		t.Fatalf("reading the verifier this build ships: %v", err)
	}
	if len(one.Brief) > BriefLimit {
		t.Errorf("the verifier's brief is %d bytes and the ceiling is %d, so the system refuses it at import",
			len(one.Brief), BriefLimit)
	}
	if len(one.Brief) < 9000 {
		t.Fatalf("the verifier's brief is %d bytes, which is smaller than the one this method was added to",
			len(one.Brief))
	}
	t.Logf("the verifier's brief is %d bytes, leaving %d under the ceiling",
		len(one.Brief), BriefLimit-len(one.Brief))
}
