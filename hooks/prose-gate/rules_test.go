package main

import (
	"strings"
	"testing"
)

// What the gate refuses comes first, and it comes first on purpose. A gate that always passes
// satisfies every test about passing, so a suite that opens with the prose it allows proves nothing
// until something has been seen to be refused.

// Every rule this gate holds, each with prose that breaks it.
//
// One table, so what the gate refuses is a list anybody can read and argue with rather than
// behaviour you have to run a container to find out.
func TestTheGateRefusesProseTheStandardRefuses(t *testing.T) {
	for _, one := range []struct {
		name string
		rule string
		text string
		says string
	}{
		{
			name: "a sentence longer than the standard allows",
			rule: "length",
			text: "The control plane reads the row and answers the question the caller asked, and it does " +
				"that before the session starts, because a session that starts on a row nobody read is " +
				"a session nobody can account for.",
			says: "38 words",
		},
		{
			name: "a paragraph of more than six sentences",
			rule: "paragraph",
			text: "One. Two things happen. Three is the count. Four comes next. Five is here. Six follows it. Seven is too many.",
			says: "7 sentences",
		},
		{
			name: "the present perfect",
			rule: "tense",
			text: "The gate has refused the command.",
			says: `"has refused" is the present perfect`,
		},
		{
			name: "the present perfect of an irregular verb",
			rule: "tense",
			text: "The task has run.",
			says: `"has run" is the present perfect`,
		},
		{
			name: "the past perfect",
			rule: "tense",
			text: "The session had written the file before the check.",
			says: `"had written" is the past perfect`,
		},
		{
			name: "an adverb between the auxiliary and the verb",
			rule: "tense",
			text: "The system has already shipped it.",
			says: `"has already shipped" is the present perfect`,
		},
		{
			name: "the present continuous",
			rule: "tense",
			text: "The task is running.",
			says: `"is running" is a continuous tense`,
		},
		{
			name: "the past continuous",
			rule: "tense",
			text: "The controller was reading the row.",
			says: `"was reading" is a continuous tense`,
		},
		{
			name: "an em dash",
			rule: "dash",
			text: "The gate reads the command—and then it answers.",
			says: "uses a dash as punctuation",
		},
		{
			name: "an en dash",
			rule: "dash",
			text: "The gate reads the command – then it answers.",
			says: "uses a dash as punctuation",
		},
		{
			name: "a hyphen standing as a dash",
			rule: "dash",
			text: "The gate reads the command - then it answers.",
			says: "uses a dash as punctuation",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			found := Check("docs/EXAMPLE.md", one.text)
			if len(found) == 0 {
				t.Fatalf("the gate allowed it, and the standard does not:\n%s", one.text)
			}
			var carried Finding
			for _, candidate := range found {
				if candidate.Rule == one.rule {
					carried = candidate
				}
			}
			if carried.Rule == "" {
				t.Fatalf("nothing was refused under %q, only %v", one.rule, rules(found))
			}
			if !strings.Contains(carried.String(), one.says) {
				t.Errorf("the refusal does not say %q, so the writer is left guessing:\n%s",
					one.says, carried)
			}
		})
	}
}

// A refusal that does not say what to do is a session that writes the same sentence again in
// different words until its budget runs out.
func TestEveryRefusalSaysWhatToDoAboutIt(t *testing.T) {
	found := Check("docs/EXAMPLE.md",
		"The gate has refused the command, and it did that because the command merges a pull "+
			"request, which is the one thing every brief in this system says a session never does.")
	if len(found) == 0 {
		t.Fatal("nothing was refused, so this test proves nothing about refusals")
	}
	for _, one := range found {
		switch {
		case one.Do == "":
			t.Errorf("%q says what is wrong and not what to do about it", one.What)
		case one.Quote == "":
			t.Errorf("%q refuses prose it does not quote, so the writer has to go and find it", one.What)
		case one.Line == 0:
			t.Errorf("%q refuses prose it does not place", one.What)
		}
	}
}

// The other direction, and the one that decides whether this gate is worth attaching. A gate that
// refuses wrongly blocks the work, and prose is what every role in this system produces.
func TestTheGateAllowsProseWrittenInTheStandard(t *testing.T) {
	for _, one := range []struct {
		name string
		text string
	}{
		{
			name: "short sentences in the simple present",
			text: "The gate reads the command. It refuses a merge. It says what to do instead.",
		},
		{
			name: "the simple past",
			text: "The session wrote the file. The gate read it. The gate found no fault.",
		},
		{
			name: "an imperative",
			text: "Push the branch. Open a pull request. Ask the operator to merge it.",
		},
		{
			// "There is nothing to prove" is a sentence this repository writes constantly, and it
			// is not a continuous tense.
			name: "a noun that ends in ing",
			text: "There is nothing to prove. The answer is something a person decides.",
		},
		{
			// A state rather than an action. A writer told to put this in the simple present has
			// been refused something they would argue with.
			name: "an adjective that ends in ing",
			text: "The file is missing. The work is outstanding. The change is pending.",
		},
		{
			name: "a word that ends in ed and is not a verb",
			text: "The machine has speed. The queue has a hundred rows.",
		},
		{
			name: "a hyphen inside a word",
			text: "The branch is named in kebab-case. The hook is a well-known shape.",
		},
		{
			name: "a command in a code span, whatever is inside it",
			text: "Run `krewe hook detach system merge-gate` to take the gate off the system.",
		},
		{
			name: "a file name, which carries a full stop and does not end a sentence",
			text: "The manifest is hook.yaml and the entry point is bin/hook. Both are read.",
		},
		{
			name: "an address, which carries full stops of its own",
			text: "The standard is at https://www.asd-ste100.org/. A person reads it.",
		},
		{
			name: "an abbreviation",
			text: "Some words, e.g. the ones in the dictionary, have one meaning. Use them.",
		},
		{
			name: "a heading, which is not a sentence",
			text: "# A heading here is not a sentence and it is not measured as one at all, ever\n\nThe gate reads the prose.",
		},
		{
			name: "a fenced code block, which is source rather than prose",
			text: "The command is below.\n\n```\ngo test -count=1 ./... && echo \"the whole suite ran, every package of it, and it was green\"\n```\n",
		},
		{
			name: "a list, where each item is its own paragraph",
			text: "The gate reads these.\n\n- One.\n- Two.\n- Three.\n- Four.\n- Five.\n- Six.\n- Seven.\n- Eight.\n",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			if found := Check("docs/EXAMPLE.md", one.text); len(found) > 0 {
				t.Errorf("the gate refused prose the standard allows:\n%s", found[0])
			}
		})
	}
}

// A sentence of exactly the limit goes through, and one word more does not. The boundary is where a
// threshold is worth testing: an off by one here refuses a sentence the standard allows.
func TestTheLengthBoundaryIsTheStandardsOwnNumber(t *testing.T) {
	words := make([]string, 0, MaxWords+1)
	for len(words) < MaxWords {
		words = append(words, "word")
	}
	if found := Check("docs/EXAMPLE.md", strings.Join(words, " ")+"."); len(found) > 0 {
		t.Errorf("a sentence of %d words was refused, and the standard allows %d: %s",
			MaxWords, MaxWords, found[0])
	}
	words = append(words, "word")
	if found := Check("docs/EXAMPLE.md", strings.Join(words, " ")+"."); len(found) == 0 {
		t.Errorf("a sentence of %d words was allowed, and the standard allows %d", MaxWords+1, MaxWords)
	}
}

func rules(found []Finding) []string {
	out := make([]string, 0, len(found))
	for _, one := range found {
		out = append(out, one.Rule)
	}
	return out
}
