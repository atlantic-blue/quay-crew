package skill

import (
	"strings"
	"testing"
)

// flowed reads a brief as the prose it is rather than as the lines it happens to be wrapped into, so
// a reflow that changes no words cannot fail a test about what the brief says.
func flowed(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// The Simplified Technical English skill is the first shipped one that is off by default: holding it
// is not a reason to use it. Every other skill describes how something is done whenever it is done,
// and this one describes a way of writing the operator has to ask for.
//
// That gate lives in the brief and nowhere else, because a skill is attached to a workspace and every
// session in it then holds the text. So the words that task it off are load bearing in a way no other
// skill's words are, and this is what holds them there.
func TestTheShippedSteSkillIsOffUntilItIsAskedFor(t *testing.T) {
	ste := shippedSkill(t, "ste")

	brief := flowed(ste.Brief)
	for _, said := range []string{"do not write this way by default", "only when the operator asks"} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, so a session holding it will simply write this way", said)
		}
	}
	// The summary is what a listing and the wizard show, and somebody deciding whether to attach it
	// reads that rather than the brief.
	if !strings.Contains(strings.ToLower(ste.Summary), "request") {
		t.Errorf("the summary does not say it is on request: %q", ste.Summary)
	}
}

// The reason it is off by default. Simplified Technical English strips rhythm and register, which is
// what a blog post is made of, and this crew's context is a voice specification: a skill that quietly
// flattened a draft would be undoing the thing the operator cares most about.
func TestTheShippedSteSkillRefusesWritingMeantToBeEnjoyed(t *testing.T) {
	brief := flowed(shippedSkill(t, "ste").Brief)

	for _, kept := range []string{"blog post", "newsletter", "marketing", "voice"} {
		if !strings.Contains(brief, kept) {
			t.Errorf("the brief never names %q as something it does not touch", kept)
		}
	}
	// Where the skill and a context disagree, the context wins. Without this a workspace that
	// describes how its writing should sound is in a fight with a skill, and nothing says who wins.
	if !strings.Contains(brief, "the context wins") {
		t.Error("the brief does not say a context outranks it, so a session has to guess")
	}
}

// The rules themselves, so the brief cannot be reduced to a warning about when not to use it.
func TestTheShippedSteSkillCarriesTheRules(t *testing.T) {
	brief := flowed(shippedSkill(t, "ste").Brief)

	for _, rule := range []string{
		"one word, one meaning",
		"active voice",
		"simple tenses",
		"one instruction per sentence",
	} {
		if !strings.Contains(brief, rule) {
			t.Errorf("the brief never states the rule %q", rule)
		}
	}
	// The standard is somebody else's copyright and its dictionary is not ours to ship, so the brief
	// has to say what it is not before anybody builds certified documentation on it.
	if !strings.Contains(brief, "asd-ste100.org") {
		t.Error("the brief does not point at the official standard as the source of truth")
	}
}

// This skill needs nothing from the sandbox, which is worth pinning: a brief that declared a binary
// or a secret it never uses would refuse tasks for no reason.
func TestTheShippedSteSkillAsksForNothing(t *testing.T) {
	ste := shippedSkill(t, "ste")

	if len(ste.Binaries) != 0 {
		t.Errorf("it declares binaries it does not need: %v", ste.Binaries)
	}
	if len(ste.Secrets) != 0 {
		t.Errorf("it declares secrets it does not need: %v", ste.Secrets)
	}
}

func shippedSkill(t *testing.T, name string) Skill {
	t.Helper()
	skills, err := Load("../../skills")
	if err != nil {
		t.Fatalf("loading the shipped skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("no skills found in skills/, so this test proves nothing")
	}
	for _, found := range skills {
		if found.Name == name {
			return found
		}
	}
	t.Fatalf("skills/ does not hold the %s skill", name)
	return Skill{}
}
