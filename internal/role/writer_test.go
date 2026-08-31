package role

import (
	"path/filepath"
	"strings"
	"testing"
)

// Fifteen roles wrote contracts, tests, code, infrastructure, security findings and a marketing
// plan, and none of them wrote prose for a person outside the work. So every writing job typed the
// method into its own brief: read the voice specification, read three existing pieces, do not use
// these words, state the cost, invent no number. That brief ran to over a thousand words and almost
// none of it was about the subject.
//
// The writer holds the method instead. These are the sentences it is made of. They are written here
// rather than derived from the file, because a check that read its expectations out of the brief
// would agree with whatever the brief said.

// writerRefusals are the two drafts the role refuses to hand over, and they come first. A role that
// accepts everything passes every test about producing a draft, and the refusals are the whole point
// of having one.
var writerRefusals = []string{
	// The uncited figure. "It came to over a thousand words" is a sentence somebody measured or a
	// sentence somebody felt, and on the page the two look the same.
	"You hold no numbers of your own.",
	"A figure that is not in the material does not go in the piece",
	"Say the figure is missing and name it, rather than estimating it.",

	// The draft that only says what worked. A reader who spots a pitch stops believing the part that
	// was true, so the cost is not a courtesy, it is what the rest of the piece rests on.
	"A draft that states no cost is not a draft",
	"Say what was skipped, what does not work yet, or where you were wrong.",
}

// writerMethod is everything the brief carries so that a job's brief does not have to: what is read
// before a word is written, the form rules that apply to every piece, and where length comes from.
var writerMethod = []string{
	// Read first. A voice is observed rather than described, and a draft written without reading is
	// the fluent anonymous piece this role exists to prevent.
	"Read the voice specification in full before you write a word.",
	"Read the three most recent pieces already published in the repository you are writing for.",
	"Where there is no voice specification and no published piece, say so and stop.",

	// The form rules. Each one was typed into a brief by hand at least once.
	"Spell every acronym out.",
	"No dash as punctuation.",
	"No table.",
	"No blockquote.",
	"No profanity.",

	// The surface decides the length, so a role that fixed one length would be wrong for every
	// surface but one.
	"The surface decides the length and the pronoun.",

	// Sending is a person's decision, and this role hands over a draft rather than taking it.
	"You never publish.",
}

// missingFromBrief is every sentence of a list the brief does not carry, in the order they are
// declared, so a failure names what to write rather than only that something is absent.
func missingFromBrief(brief string, wanted []string) []string {
	var absent []string
	for _, sentence := range wanted {
		if !strings.Contains(brief, sentence) {
			absent = append(absent, sentence)
		}
	}
	return absent
}

// theWriter is the role this build ships, read off disk.
func theWriter(t *testing.T) Role {
	t.Helper()
	one, err := One(filepath.Join(shipped, "writer"))
	if err != nil {
		t.Fatalf("reading the writer this build ships: %v", err)
	}
	return one
}

// The check that the check works, first, because a guard that cannot fail passes over anything.
//
// A brief with one sentence cut out is exactly what a later edit produces, so the guard is watched
// catching that before it is trusted about the file that ships. An empty brief is the other end of
// it: a role whose file failed to load reads as a clean sweep to any check that only asks what is
// absent.
func TestTheWriterCheckCatchesASentenceTakenOutOfTheBrief(t *testing.T) {
	brief := theWriter(t).Brief
	all := append(append([]string{}, writerRefusals...), writerMethod...)

	for _, sentence := range all {
		cut := strings.Replace(brief, sentence, "", 1)
		if cut == brief {
			t.Errorf("cutting %q changed nothing, so the guard is looking for a sentence that is not there", sentence)
			continue
		}
		absent := missingFromBrief(cut, all)
		if len(absent) != 1 || absent[0] != sentence {
			t.Errorf("with %q cut, the guard reports %v", sentence, absent)
		}
	}

	if got := len(missingFromBrief("", all)); got != len(all) {
		t.Errorf("an empty brief is missing %d of the %d sentences, so a role that failed to load would read as one that carries the method",
			got, len(all))
	}
}

// The first refusal: a draft carrying a figure the material does not.
//
// The draft this catches reads "the brief came to over a thousand words". Nobody counted. An
// estimate is the shape this fails in, because "about", "roughly" and "several times" each read
// exactly like a number somebody measured, and the reader has no way to tell which they are holding.
func TestTheWriterRefusesAFigureThatIsNotInTheMaterial(t *testing.T) {
	brief := theWriter(t).Brief
	for _, sentence := range missingFromBrief(brief, writerRefusals[:3]) {
		t.Errorf("the writer's brief does not say %q, so a session running as it estimates rather than refusing", sentence)
	}
	// The way out has to be in the brief too. A refusal with no next move is a session that stops
	// mid draft and says nothing anybody can act on.
	for _, said := range []string{"write the sentence without a figure, or ask for the figure and wait"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief refuses the figure and does not say %q, so the refusal ends the job rather than the sentence", said)
		}
	}
}

// The second refusal: a draft that says only what worked.
//
// This is the one that looks finished. Every sentence is true, the piece reads well, and it is a
// pitch. The cost goes inside the piece rather than in a note beside it, because the note is the
// part a reader skips.
func TestTheWriterRefusesADraftThatStatesNoCost(t *testing.T) {
	brief := theWriter(t).Brief
	for _, sentence := range missingFromBrief(brief, writerRefusals[3:]) {
		t.Errorf("the writer's brief does not say %q, so a draft that only says what worked is handed over", sentence)
	}
	// And the other direction, which is the refusal turning into its own failure mode: a role told to
	// state a cost, given material carrying none, invents one.
	if !strings.Contains(brief, "Never invent a cost to satisfy this rule") {
		t.Error("the brief does not refuse an invented cost, so the rule about stating one manufactures the thing the other rule refuses")
	}
}

// And the method, which is what a brief no longer has to carry.
func TestTheWriterCarriesTheMethodSoABriefDoesNotHaveTo(t *testing.T) {
	brief := theWriter(t).Brief
	for _, sentence := range missingFromBrief(brief, writerMethod) {
		t.Errorf("the writer's brief does not say %q, so the next writing job types it out again", sentence)
	}
	// The sentence that tells a session which of the two to follow when a brief written before this
	// role existed repeats the rules and gets one of them slightly wrong.
	if !strings.Contains(brief, "where the two differ this file wins") {
		t.Error("the brief does not say it outranks a job's brief on method, so a stale brief quietly overrides the role")
	}
	t.Logf("the writer's brief carries all %d sentences of the method and all %d refusals",
		len(writerMethod), len(writerRefusals))
}

// What the role is, held here rather than only in the table every shipped role is in, because these
// three decide whether the refusals above can be kept at all.
//
// It runs on opus: writing in somebody's voice is the job the larger model is worth, the same reason
// the marketing pair runs there. It receives context, because the voice specification reaches a
// session that way. It grants no verb, which is the whole of "it holds no numbers of its own": a
// role that could read the system's own job records would have a second source of figures, and the
// material would stop being the only one.
func TestTheWriterRunsOnOpusReceivesTheContextAndMayCallNothing(t *testing.T) {
	one := theWriter(t)
	if one.Model != "opus" {
		t.Errorf("the writer runs on %q", one.Model)
	}
	for _, material := range []string{MaterialJob, MaterialContext, MaterialSkills} {
		if !one.Gets(material) {
			t.Errorf("the writer does not receive %s, and it receives %s", material, strings.Join(one.Receives, ", "))
		}
	}
	if len(one.Verbs) != 0 {
		t.Errorf("the writer may %s, and a role with a second source of figures cannot hold the material to being the only one",
			strings.Join(one.Verbs, ", "))
	}
}

// The ceiling. A brief over it is refused at import, so the role would stop shipping at all. The room
// left is reported rather than only the size, because the next edit to this file is the one that has
// to know how much there is. A floor is asserted too: a brief that failed to load is nought bytes,
// which is under any ceiling.
func TestTheWriterBriefStaysUnderTheCeiling(t *testing.T) {
	one := theWriter(t)
	if len(one.Brief) > BriefLimit {
		t.Errorf("the writer's brief is %d bytes and the ceiling is %d, so the system refuses it at import",
			len(one.Brief), BriefLimit)
	}
	if len(one.Brief) < 4000 {
		t.Fatalf("the writer's brief is %d bytes, which is too small to be carrying the method at all", len(one.Brief))
	}
	t.Logf("the writer's brief is %d bytes, leaving %d under the ceiling", len(one.Brief), BriefLimit-len(one.Brief))
}
