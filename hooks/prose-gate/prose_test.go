package main

import (
	"strings"
	"testing"
)

// The reader is where a gate like this actually goes wrong. Almost every wrong refusal it can make
// is made here rather than in the rules: a code fence read as prose is a wall of refusals about
// source, a list read as one block is a paragraph of forty sentences, and a file name read as a full
// stop cuts one sentence into three or joins two into one that is over the limit.
//
// So what it drops comes first.

func TestTheReaderDropsWhatIsNotProse(t *testing.T) {
	for _, one := range []struct {
		name   string
		source string
	}{
		{
			name:   "a fenced code block",
			source: "```\nfunc main() { fmt.Println(\"one\"); fmt.Println(\"two\"); fmt.Println(\"three\") }\n```",
		},
		{
			name:   "a fenced block that names its language",
			source: "```mermaid\nflowchart LR\n    A[\"one\"] --> B[\"two\"]\n```",
		},
		{name: "a heading", source: "## The shape of a hook"},
		{name: "a table row", source: "| command | what it does |"},
		{name: "a horizontal rule", source: "---"},
		{name: "an indented command", source: "    make hooks"},
		{name: "front matter", source: "---\nname: prose-gate\nsummary: one line\n---\n"},
	} {
		t.Run(one.name, func(t *testing.T) {
			if found := Paragraphs(one.source); len(found) > 0 {
				t.Errorf("the reader read %d paragraphs of prose in it: %q",
					len(found), found[0].Sentences[0].Text)
			}
		})
	}
}

// A list is a run of items in markdown and a run of paragraphs to a reader. Measured as one block,
// every list of more than six things is a paragraph the gate refuses.
func TestEachListItemIsItsOwnParagraph(t *testing.T) {
	found := Paragraphs("- One.\n- Two.\n- Three.\n")
	if len(found) != 3 {
		t.Fatalf("a list of three items read as %d paragraphs", len(found))
	}
	numbered := Paragraphs("1. One.\n2. Two.\n")
	if len(numbered) != 2 {
		t.Fatalf("a numbered list of two items read as %d paragraphs", len(numbered))
	}
}

// Where one sentence ends and the next begins, which is what the length rule measures. A cut in the
// wrong place makes a sentence longer or shorter than the writer wrote.
func TestWhereTheReaderEndsASentence(t *testing.T) {
	for _, one := range []struct {
		name   string
		source string
		want   int
	}{
		{name: "two sentences", source: "The gate reads it. It answers.", want: 2},
		{name: "a file name in the middle", source: "The manifest is hook.yaml and it is data. It is read once.", want: 2},
		{name: "a version number", source: "The module needs go 1.25.0 and nothing else. That is the whole of it.", want: 2},
		{name: "an abbreviation", source: "Some words, e.g. these ones, have one meaning. Use them.", want: 2},
		{name: "an address at the end of a sentence", source: "The standard is at https://www.asd-ste100.org/. A person reads it.", want: 2},
		{name: "a question and an answer", source: "Does it fire? It does. Every time.", want: 3},
		{name: "a quoted sentence", source: `The brief says "never merge." The gate says the same.`, want: 2},
		{name: "a paragraph broken across lines", source: "The gate reads the\ncommand. It answers.", want: 2},
	} {
		t.Run(one.name, func(t *testing.T) {
			found := Paragraphs(one.source)
			if len(found) != 1 {
				t.Fatalf("read as %d paragraphs, want 1", len(found))
			}
			if got := len(found[0].Sentences); got != one.want {
				var said []string
				for _, s := range found[0].Sentences {
					said = append(said, s.Text)
				}
				t.Errorf("read as %d sentences, want %d:\n  %s", got, one.want, strings.Join(said, "\n  "))
			}
		})
	}
}

// A code span is one term however many words are inside it. Counted word by word, a command in the
// middle of a sentence is five nouns in a row and the sentence is over the limit.
func TestACodeSpanIsOneWord(t *testing.T) {
	found := Paragraphs("Run `krewe hook detach system merge-gate` now.")
	if len(found) != 1 {
		t.Fatalf("read as %d paragraphs", len(found))
	}
	if words := found[0].Sentences[0].Words; len(words) != 3 {
		t.Errorf("counted %d words in it: %v", len(words), words)
	}
}

// A refusal points at a place, so the writer does not have to search the document for the sentence.
func TestAParagraphCarriesTheLineItStartsOn(t *testing.T) {
	found := Paragraphs("# A heading\n\nThe first paragraph.\n\nThe second one.\n")
	if len(found) != 2 {
		t.Fatalf("read as %d paragraphs", len(found))
	}
	if found[0].Line != 3 || found[1].Line != 5 {
		t.Errorf("the paragraphs start on lines %d and %d, and they start on 3 and 5",
			found[0].Line, found[1].Line)
	}
}
