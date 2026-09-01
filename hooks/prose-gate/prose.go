package main

import (
	"regexp"
	"strings"
	"unicode"
)

// This file turns a piece of markdown into the units the rules measure: paragraphs, the sentences in
// them, and the words in those.
//
// Almost every false refusal a gate like this can make is made here rather than in the rules. A code
// fence read as prose is a wall of refusals about source. A list read as one block is a paragraph of
// forty sentences. A file path read as a full stop cuts one sentence into three, or joins two into
// one that is over the limit. So this reads markdown far enough to know what is prose and what is
// not, and everything it cannot read it drops rather than measures.

// A Paragraph is one block of prose and where it starts.
type Paragraph struct {
	// Line is the line of the source the block starts on, counting from one, so a refusal points at
	// a place rather than at a quotation the writer has to go and find.
	Line      int
	Sentences []Sentence
}

// A Sentence is one sentence of a paragraph.
type Sentence struct {
	Line int
	Text string
	// Words is the sentence split the way the count is made, so what the refusal says is the number
	// and what the rule measured are the same thing.
	Words []string
}

// block is a run of source lines that form one paragraph, before any of it is read as prose.
type block struct {
	line  int
	lines []string
}

// Paragraphs reads markdown and answers the prose in it.
func Paragraphs(source string) []Paragraph {
	var found []Paragraph
	for _, one := range blocks(source) {
		text := normalise(strings.Join(one.lines, " "))
		sentences := Sentences(text, one.line)
		if len(sentences) == 0 {
			continue
		}
		found = append(found, Paragraph{Line: one.line, Sentences: sentences})
	}
	return found
}

// fence opens or closes a code block, and everything between two of them is source rather than
// prose. A mermaid diagram arrives this way too.
var fence = regexp.MustCompile("^\\s*(```|~~~)")

// notProse is a line that is markdown furniture rather than a sentence: a heading, a rule, a table
// row, a link definition. None of them is measured, because none of them is a sentence somebody
// wrote to be read as one.
//
// An angle bracket is deliberately not here. Every one of them in this repository is a placeholder
// standing for a value, as in `quay job show <id>`, and dropping the line it sits on cuts the
// paragraph in half and leaves the code span around it unclosed. That was one wrong refusal about a
// dash, in prose that had no dash in it.
var notProse = regexp.MustCompile(`^\s*(#{1,6}\s|\||\[[^\]]+\]:\s|(\*\s*){3,}$|(-\s*){3,}$|(=\s*){3,}$)`)

// bullet is the marker of a list item. Each item is its own paragraph: markdown makes a run of them
// one block, and measuring the run as one paragraph would refuse every list of more than six things.
var bullet = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)

// blocks cuts the source into the runs of lines that are paragraphs, and drops everything that is
// not prose on the way.
func blocks(source string) []block {
	lines := strings.Split(source, "\n")
	var found []block
	var current block
	inFence := false

	flush := func() {
		if len(current.lines) > 0 {
			found = append(found, current)
		}
		current = block{}
	}

	start := 0
	// Front matter is data about the document rather than the document, and it is not prose.
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for at := 1; at < len(lines); at++ {
			if strings.TrimSpace(lines[at]) == "---" {
				start = at + 1
				break
			}
		}
	}

	for at := start; at < len(lines); at++ {
		line := lines[at]
		if fence.MatchString(line) {
			inFence = !inFence
			flush()
			continue
		}
		if inFence {
			continue
		}
		// An indented block under a paragraph is a code sample or a command, not a sentence.
		if strings.HasPrefix(line, "    ") && len(current.lines) == 0 {
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if notProse.MatchString(line) {
			flush()
			continue
		}
		if bullet.MatchString(line) {
			flush()
			line = bullet.ReplaceAllString(line, "")
		}
		line = strings.TrimPrefix(strings.TrimSpace(line), "> ")
		if len(current.lines) == 0 {
			current.line = at + 1
		}
		current.lines = append(current.lines, line)
	}
	flush()
	return found
}

// The replacements that turn markdown into the words a reader says out loud. Each one exists because
// leaving it in makes the sentence splitter or the word count wrong.
var (
	// A code span is one term however many words are inside it, and the words inside it are not
	// prose: `quay hook detach system merge-gate` is a command, not five nouns in a row. It becomes
	// one word, and an uppercase one, so a sentence that starts with a command still reads as the
	// start of a sentence.
	codeSpan = regexp.MustCompile("(``[^`]+``|`[^`]+`)")
	// An address carries full stops and slashes that would cut a sentence into pieces. The mark at
	// the end of it is not part of it: an address that eats the full stop after it joins two
	// sentences into one, and the length of a sentence is what this gate measures.
	address = regexp.MustCompile(`(<https?://[^>]+>|https?://[^\s]*[^\s.,;:!?)\]])`)
	image   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	link    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// Emphasis is not a word.
	emphasis = regexp.MustCompile(`\*\*|\*|__`)
	spaces   = regexp.MustCompile(`\s+`)
)

// normalise turns one paragraph of markdown into the prose in it.
func normalise(text string) string {
	text = codeSpan.ReplaceAllString(text, "CODE")
	text = address.ReplaceAllString(text, "ADDRESS")
	text = image.ReplaceAllString(text, "")
	text = link.ReplaceAllString(text, "$1")
	text = emphasis.ReplaceAllString(text, "")
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

// abbreviations end with a full stop and do not end a sentence.
var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "vs": true, "cf": true, "al": true,
	"no": true, "mr": true, "mrs": true, "ms": true, "dr": true, "st": true,
}

// Sentences cuts one paragraph into its sentences.
//
// The full stop is the hard part, because prose here is full of things that carry one and are not
// the end of anything: a version, a file name, an abbreviation. A cut in the wrong place makes a
// sentence longer or shorter than the writer wrote, and the length is what this gate measures, so a
// wrong cut is a wrong refusal.
func Sentences(text string, line int) []Sentence {
	var found []Sentence
	runes := []rune(text)
	start := 0
	for at := 0; at < len(runes); at++ {
		c := runes[at]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		// An ellipsis is one mark, and the end of it is where a sentence might end.
		for at+1 < len(runes) && (runes[at+1] == '.' || runes[at+1] == '!' || runes[at+1] == '?') {
			at++
		}
		// A closing quotation or bracket belongs to the sentence that ends.
		end := at + 1
		for end < len(runes) && strings.ContainsRune(`"')]`, runes[end]) {
			end++
		}
		// Nothing after it, or something other than a space, means this is not the end: a file name
		// and a version both carry a full stop with a letter or a digit on both sides of it.
		if end < len(runes) && !unicode.IsSpace(runes[end]) {
			continue
		}
		piece := string(runes[start:end])
		if c == '.' && abbreviates(piece) {
			continue
		}
		if sentence, ok := sentenceOf(piece, line); ok {
			found = append(found, sentence)
		}
		start = end
	}
	if sentence, ok := sentenceOf(string(runes[start:]), line); ok {
		found = append(found, sentence)
	}
	return found
}

// abbreviates says whether this piece ends in something that carries a full stop and carries on.
func abbreviates(piece string) bool {
	words := strings.Fields(piece)
	if len(words) == 0 {
		return false
	}
	last := strings.ToLower(strings.TrimSuffix(words[len(words)-1], "."))
	if abbreviations[last] {
		return true
	}
	// A single letter with a full stop after it is an initial rather than the end of a sentence.
	return len([]rune(last)) == 1 && unicode.IsLetter([]rune(last)[0])
}

// sentenceOf makes a sentence out of a piece, or says there is no sentence in it. A piece with no
// letter in it is punctuation left over from something this reader dropped.
func sentenceOf(piece string, line int) (Sentence, bool) {
	text := strings.TrimSpace(piece)
	words := Words(text)
	if len(words) == 0 {
		return Sentence{}, false
	}
	return Sentence{Line: line, Text: text, Words: words}, true
}

// Words are the words of a sentence, which is what the length rule counts. A token with no letter
// and no digit in it is a mark rather than a word.
func Words(text string) []string {
	var found []string
	for _, token := range strings.Fields(text) {
		if strings.ContainsFunc(token, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			found = append(found, token)
		}
	}
	return found
}

// bare is a word with the punctuation around it taken off and its case dropped, which is the form
// every word list here is written in.
func bare(word string) string {
	return strings.ToLower(strings.Trim(word, `.,;:!?"'()[]{}`))
}
